package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// fakeCursorAPI keeps repository metadata on details, as the Cursor API does.
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

func getAgents(t *testing.T, base string) AgentsResponse {
	t.Helper()
	return getAgentsAt(t, base+"/api/agents")
}

func getAgentsAt(t *testing.T, url string) AgentsResponse {
	t.Helper()
	resp := authedGet(t, url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out AgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGetAgentsMergesIDEAndCloud(t *testing.T) {
	now := time.Now()
	srv, ts := newTestServerWithSessions(t, []*model.Session{
		{SessionID: "ide-1", Agent: "cursor", CWD: t.TempDir(), Name: "refactor", LastActivity: now},
	})
	defer ts.Close()

	var calls atomic.Int32
	cursor := fakeCursorAPI(t, &calls)
	defer cursor.Close()
	srv.cloud = newCloudAgents(cursor.URL, "key")

	active := getAgents(t, ts.URL)
	if len(active.Errors) != 0 {
		t.Fatalf("errors = %v", active.Errors)
	}
	if len(active.Agents) != 1 || active.Agents[0].ID != "bc-1" {
		t.Fatalf("default list = %+v, want only the active cloud agent", active.Agents)
	}
	if got := active.Agents[0]; got.Source != "cloud" || got.Status != agentWorking || got.Repo != "https://github.com/o/r" {
		t.Errorf("cloud item = %+v", got)
	}

	byID := map[string]AgentItem{}
	for _, a := range getAgentsAt(t, ts.URL+"/api/agents?all=true").Agents {
		byID[a.ID] = a
	}
	if len(byID) != 3 {
		t.Fatalf("all=true ids = %v, want ide-1, bc-1, bc-2", byID)
	}
	if got := byID["ide-1"]; got.Source != "ide" || got.Name != "refactor" || got.Status != agentIdle {
		t.Errorf("ide item = %+v", got)
	}
}

func TestGetAgentsCachesCloudCalls(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	var calls atomic.Int32
	cursor := fakeCursorAPI(t, &calls)
	defer cursor.Close()
	srv.cloud = newCloudAgents(cursor.URL, "key")

	getAgents(t, ts.URL)
	getAgents(t, ts.URL)
	if got := calls.Load(); got != 3 {
		t.Errorf("cursor api calls = %d, want one list and two details", got)
	}
}

func TestGetAgentsFollowsAllCursorPages(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	var pages atomic.Int32
	const cursor = "next/+ page?key=value&other"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents" {
			w.Write([]byte(`{"repos":[{"url":"https://github.com/o/r"}]}`))
			return
		}
		pages.Add(1)
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q, want 100", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("cursor") {
		case "":
			items := make([]map[string]string, 100)
			for i := range items {
				items[i] = map[string]string{"id": fmt.Sprintf("idle-%d", i), "status": "IDLE"}
			}
			json.NewEncoder(w).Encode(map[string]any{"items": items, "nextCursor": cursor})
		case cursor:
			w.Write([]byte(`{"items":[],"nextCursor":"last"}`))
		case "last":
			w.Write([]byte(`{"items":[{"id":"bc-active","status":"ACTIVE"},{"id":"idle-0","status":"IDLE"}]}`))
		default:
			t.Errorf("unexpected cursor %q", r.URL.Query().Get("cursor"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer upstream.Close()
	srv.cloud = newCloudAgents(upstream.URL, "key")

	out := getAgents(t, ts.URL)
	if len(out.Errors) != 0 || len(out.Agents) != 1 || out.Agents[0].ID != "bc-active" {
		t.Fatalf("default response = %+v, want active agent beyond the first 100", out)
	}
	if out.Agents[0].Repo != "https://github.com/o/r" {
		t.Errorf("repository = %q", out.Agents[0].Repo)
	}
	all := getAgentsAt(t, ts.URL+"/api/agents?all=true")
	if len(all.Agents) != 101 || len(all.Errors) != 0 {
		t.Errorf("all response has %d agents and errors %v, want 101 unique agents", len(all.Agents), all.Errors)
	}
	if got := pages.Load(); got != 3 {
		t.Errorf("list calls = %d, want 3 (including an empty page)", got)
	}
}

func TestGetAgentsKeepsPartialCloudResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		page string
		code int
		want string
	}{
		{name: "failed page", code: http.StatusBadGateway, want: "cursor api returned 502"},
		{name: "invalid page", page: "{", want: "unexpected EOF"},
		{name: "repeated cursor", page: `{"items":[],"nextCursor":"next"}`, want: "repeated a cursor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, ts := newTestServerWithSessions(t, []*model.Session{
				{SessionID: "ide-1", Agent: "cursor", CWD: t.TempDir(), LastActivity: time.Now(), Status: model.StatusThinking},
			})
			defer ts.Close()
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/v1/agents" {
					w.Write([]byte(`{"repos":[]}`))
				} else if r.URL.Query().Get("cursor") == "" {
					w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE"}],"nextCursor":"next"}`))
				} else if tc.code != 0 {
					w.WriteHeader(tc.code)
				} else {
					w.Write([]byte(tc.page))
				}
			}))
			defer upstream.Close()
			srv.cloud = newCloudAgents(upstream.URL, "key")

			for range 2 {
				out := getAgents(t, ts.URL)
				if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], tc.want) {
					t.Fatalf("errors = %v, want %q", out.Errors, tc.want)
				}
				if len(out.Agents) != 2 || out.Agents[0].ID != "ide-1" || out.Agents[1].ID != "bc-1" {
					t.Fatalf("agents = %+v, want IDE and partial cloud results", out.Agents)
				}
			}
			if got := calls.Load(); got != 3 {
				t.Errorf("calls = %d, want two pages and one detail (partial response cached)", got)
			}
		})
	}
}

func TestGetAgentsKeepsCloudAgentOnDetailError(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code int
		want string
	}{
		{name: "failed detail", code: http.StatusNotFound, want: "cursor api returned 404"},
		{name: "invalid detail", body: "{", want: "unexpected EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, ts := newTestServer(t)
			defer ts.Close()
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/v1/agents" {
					w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE"}]}`))
				} else if tc.code != 0 {
					w.WriteHeader(tc.code)
				} else {
					w.Write([]byte(tc.body))
				}
			}))
			defer upstream.Close()
			srv.cloud = newCloudAgents(upstream.URL, "key")
			for range 2 {
				out := getAgents(t, ts.URL)
				if len(out.Agents) != 1 || out.Agents[0].ID != "bc-1" || out.Agents[0].Repo != "" {
					t.Fatalf("agents = %+v, want cloud agent without metadata", out.Agents)
				}
				if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], tc.want) {
					t.Fatalf("errors = %v, want detail error %q", out.Errors, tc.want)
				}
			}
			if got := calls.Load(); got != 2 {
				t.Errorf("calls = %d, want list and failed detail (failure cached)", got)
			}
		})
	}
}

func TestGetAgentsSlowCloudRefreshContinues(t *testing.T) {
	srv, ts := newTestServerWithSessions(t, []*model.Session{
		{SessionID: "ide-1", Agent: "cursor", CWD: t.TempDir(), LastActivity: time.Now(), Status: model.StatusThinking},
	})
	defer ts.Close()
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-release:
			w.Write([]byte(`{"items":[{"id":"bc-1","status":"ACTIVE","repos":[]}]}`))
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	srv.cloud = newCloudAgents(upstream.URL, "key", 10*time.Millisecond)
	defer srv.cloud.Close()
	for range 2 {
		out := getAgents(t, ts.URL)
		if len(out.Errors) != 1 || !strings.Contains(out.Errors[0], "refresh in progress") {
			t.Fatalf("errors = %v, want an explicit pending refresh", out.Errors)
		}
		if len(out.Agents) != 1 || out.Agents[0].ID != "ide-1" {
			t.Fatalf("agents = %+v, want live IDE results while cloud refresh continues", out.Agents)
		}
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		response := getAgents(t, ts.URL)
		if len(response.Errors) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background refresh did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	out := getAgents(t, ts.URL)
	if len(out.Errors) != 0 || len(out.Agents) != 2 {
		t.Fatalf("completed response = %+v, want IDE and cloud agents without errors", out)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want one refresh shared across all polls", got)
	}
}

func TestGetAgentsReportsCloudErrorButKeepsIDE(t *testing.T) {
	srv, ts := newTestServerWithSessions(t, []*model.Session{
		{SessionID: "ide-1", Agent: "cursor", CWD: t.TempDir(), Name: "refactor", LastActivity: time.Now()},
	})
	defer ts.Close()

	calls := 0
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	srv.cloud = newCloudAgents(broken.URL, "key")

	out := getAgentsAt(t, ts.URL+"/api/agents?all=true")
	if len(out.Errors) != 1 {
		t.Errorf("errors = %v, want one cloud error", out.Errors)
	}
	if len(out.Agents) != 1 || out.Agents[0].ID != "ide-1" {
		t.Errorf("agents = %+v, want the IDE agent kept", out.Agents)
	}

	if again := getAgents(t, ts.URL); len(again.Errors) != 1 {
		t.Errorf("cached errors = %v, want the cloud error again", again.Errors)
	}
	if calls != 1 {
		t.Errorf("cursor api calls = %d, want 1 (failure cached)", calls)
	}
}

func TestGetAgentsWithoutKeySkipsCloud(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	out := getAgents(t, ts.URL)
	if len(out.Errors) != 0 {
		t.Errorf("errors = %v, want none when no key is set", out.Errors)
	}
}

func TestAgentsRejectsMissingToken(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGetAgentsReturnsOnlyCursorCloudURLs(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()
	cases := []struct{ id, upstream, want string }{
		{"other-host", "https://evil.example/agents/one", "https://cursor.com/agents/other-host"},
		{"script", "javascript:alert(1)", "https://cursor.com/agents/script"},
		{"userinfo", "https://user@cursor.com/agents/one", "https://cursor.com/agents/userinfo"},
		{"missing/slash", "", "https://cursor.com/agents/missing%2Fslash"},
		{"valid", "https://cursor.com/agents/valid?tab=files", "https://cursor.com/agents/valid?tab=files"},
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]any, 0, len(cases))
		for _, tc := range cases {
			items = append(items, map[string]any{"id": tc.id, "url": tc.upstream, "status": "ACTIVE", "repos": []any{}})
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer upstream.Close()
	srv.cloud = newCloudAgents(upstream.URL, "fixture-key")
	defer srv.cloud.Close()
	out := getAgents(t, ts.URL)
	if len(out.Errors) != 0 || len(out.Agents) != len(cases) {
		t.Fatalf("cloud response: %+v", out)
	}
	byID := make(map[string]string)
	for _, item := range out.Agents {
		byID[item.ID] = item.URL
	}
	for _, tc := range cases {
		if got := byID[tc.id]; got != tc.want {
			t.Errorf("%s URL = %q, want %q", tc.id, got, tc.want)
		}
	}
}
