package cloud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeCursorAPI(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if user, password, ok := r.BasicAuth(); !ok || user != "key" || password != "" {
			t.Error("missing or incorrect Cursor authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/agents/bc-1":
			w.Write([]byte(`{"repos":[{"url":"https://github.com/o/r"}]}`))
			return
		case "/v1/agents/bc-2":
			w.Write([]byte(`{"repos":[]}`))
			return
		case "/v1/agents":
		default:
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"items":[
			{"id":"bc-1","name":"cloud run","status":"ACTIVE","url":"https://cursor.com/agents/bc-1","updatedAt":"2026-11-03T20:00:00Z"},
			{"id":"bc-2","name":"old","status":"IDLE","updatedAt":"2026-11-01T20:00:00Z"}
		]}`))
	}))
}

func TestCloudAgentsCachesRepositoryMetadata(t *testing.T) {
	var calls atomic.Int32
	upstream := fakeCursorAPI(t, &calls)
	defer upstream.Close()
	cloud := NewCursor(upstream.URL, "key")
	for round, wantCalls := range []int32{3, 4, 6} {
		items, err := cloud.List()
		if err != nil || len(items) != 2 || items[0].Repo != "https://github.com/o/r" || items[1].Repo != "" {
			t.Fatalf("round %d: items = %+v, error = %v", round, items, err)
		}
		if got := calls.Load(); got != wantCalls {
			t.Fatalf("round %d: calls = %d, want %d", round, got, wantCalls)
		}
		cloud.fetched = time.Now().Add(-cloudCacheTTL)
		if round == 1 {
			cached := cloud.repos["bc-1"]
			cached.fetched = time.Now().Add(-cloudRepoTTL)
			cloud.repos["bc-1"] = cached
		}
	}
}

func TestCloudAgentsUsesInlineRepositories(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/agents" {
			t.Error("inline repositories must not trigger a detail lookup")
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"items":[
			{"id":"bc-1","status":"ACTIVE","repos":[{"url":"https://github.com/o/r"},{"url":"https://github.com/o/other"}]},
			{"id":"bc-2","status":"IDLE","repos":[]}
		]}`))
	}))
	defer upstream.Close()
	cloud := NewCursor(upstream.URL, "key")
	defer cloud.Close()
	items, err := cloud.List()
	if err != nil || len(items) != 2 || items[0].Repo != "https://github.com/o/r" || items[1].Repo != "" {
		t.Fatalf("items = %+v, error = %v", items, err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want only the list request", got)
	}
}

func TestCloudAgentsCancelBackgroundRefresh(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer upstream.Close()
	cloud := NewCursor(upstream.URL, "key")
	defer cloud.Close()
	cloud.wait = 10 * time.Millisecond
	if _, err := cloud.List(); err == nil {
		t.Fatal("expected pending refresh")
	}
	cloud.mu.Lock()
	done := cloud.refresh
	cloud.mu.Unlock()
	cloud.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background refresh ignored shutdown cancellation")
	}
	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	if !errors.Is(cloud.cachedErr, context.Canceled) {
		t.Errorf("refresh error = %v, want cancellation", cloud.cachedErr)
	}
}

func TestCloudAgentsSharesConcurrentRefresh(t *testing.T) {
	var calls atomic.Int32
	upstream := fakeCursorAPI(t, &calls)
	defer upstream.Close()
	cloud := NewCursor(upstream.URL, "key")
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := cloud.List()
			if err != nil || len(items) != 2 || items[0].Repo != "https://github.com/o/r" {
				t.Errorf("items = %+v, error = %v", items, err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want one shared list and two details", got)
	}
}

func TestCloudAgentsRetriesFailedRepositoryRefresh(t *testing.T) {
	var failDetail atomic.Bool
	var removed atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/agents" {
			if removed.Load() {
				w.Write([]byte(`{"items":[]}`))
			} else {
				w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE"}]}`))
			}
		} else if failDetail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.Write([]byte(`{"repos":[{"url":"https://github.com/o/r"}]}`))
		}
	}))
	defer upstream.Close()
	cloud := NewCursor(upstream.URL, "key")
	if _, err := cloud.List(); err != nil {
		t.Fatal(err)
	}
	cached := cloud.repos["bc-1"]
	cached.fetched = time.Now().Add(-cloudRepoTTL)
	cloud.repos["bc-1"] = cached
	cloud.fetched = time.Now().Add(-cloudCacheTTL)
	failDetail.Store(true)
	items, err := cloud.List()
	if err == nil || len(items) != 1 || items[0].Repo != "https://github.com/o/r" {
		t.Fatalf("items = %+v, error = %v; want last known repo with explicit error", items, err)
	}
	cloud.fetched = time.Now().Add(-cloudErrorTTL)
	failDetail.Store(false)
	if _, err := cloud.List(); err != nil {
		t.Fatalf("repository retry failed: %v", err)
	}
	cloud.fetched = time.Now().Add(-cloudCacheTTL)
	removed.Store(true)
	if items, err := cloud.List(); err != nil || len(items) != 0 || len(cloud.repos) != 0 {
		t.Fatalf("items = %+v, error = %v, cached repos = %v; want removed agent evicted", items, err, cloud.repos)
	}
}

func TestCloudAgentsRefreshDeadline(t *testing.T) {
	for _, phase := range []string{"page", "detail"} {
		t.Run(phase, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/agents" && r.URL.Query().Get("cursor") == "" {
					if phase == "page" {
						w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE"}],"nextCursor":"next"}`))
					} else {
						w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE"}]}`))
					}
					return
				}
				<-r.Context().Done()
			}))
			defer upstream.Close()
			cloud := NewCursor(upstream.URL, "key")
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			items, err := cloud.fetch(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || len(items) != 1 || items[0].ID != "bc-1" {
				t.Fatalf("items = %+v, error = %v; want partial result with deadline error", items, err)
			}
			if len(cloud.repos) != 0 {
				t.Errorf("failed lookups must not cache empty repository metadata: %v", cloud.repos)
			}
		})
	}
}
