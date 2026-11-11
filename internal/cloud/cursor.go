// Package cloud provides shared, cached cloud-agent discovery for every interface.
package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	CursorAPIBase     = "https://api.cursor.com"
	Working           = "working"
	NeedsYou          = "needs_you"
	Idle              = "idle"
	cloudCacheTTL     = 30 * time.Second
	cloudErrorTTL     = 5 * time.Second
	cloudFetchTimeout = 2 * time.Minute
	cloudWaitTimeout  = 10 * time.Second
	cloudHTTPTimeout  = 30 * time.Second
	cloudRepoTTL      = 5 * time.Minute
	cloudRepoWorkers  = 4
)

type Options struct{ WaitTimeout time.Duration }

// State describes discovery without exposing credentials or blocking readers.
type State struct {
	Configured bool
	Loading    bool
	Count      int
	Error      string
	UpdatedAt  time.Time
	Revision   uint64
}

// Refresh starts one asynchronous refresh, coalescing concurrent requests.
func (c *Cursor) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Enabled() || c.closed || c.refresh != nil {
		return
	}
	done := make(chan struct{})
	c.refresh = done
	go c.fetchAndCache(done)
}

// Snapshot starts a stale refresh and returns immediately. UI event loops use
// this instead of List, so slow upstream pages never delay local sessions.
func (c *Cursor) Snapshot() ([]Agent, State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := cloudCacheTTL
	if c.cachedErr != nil {
		ttl = cloudErrorTTL
	}
	if c.Enabled() && !c.closed && c.refresh == nil && (c.fetched.IsZero() || time.Since(c.fetched) >= ttl) {
		done := make(chan struct{})
		c.refresh = done
		go c.fetchAndCache(done)
	}
	state := State{Configured: c.Enabled(), Loading: c.refresh != nil, Count: len(c.cached), UpdatedAt: c.fetched, Revision: c.revision}
	if c.cachedErr != nil {
		state.Error = c.cachedErr.Error()
	}
	return slices.Clone(c.cached), state
}

// Close cancels and drains any in-flight refresh. It is safe to call twice.
func (c *Cursor) Close() {
	c.mu.Lock()
	c.closed = true
	done := c.refresh
	c.cancel()
	c.mu.Unlock()
	if done != nil {
		<-done
	}
}

// AgentURL only permits the Cursor web application, never arbitrary handlers.
func AgentURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() != "cursor.com" || u.Port() != "" || u.User != nil || !strings.HasPrefix(u.Path, "/agents/") {
		return "", errors.New("invalid Cursor cloud agent URL")
	}
	return u.String(), nil
}

// Agent is one coding agent, IDE chat or Cursor cloud agent, in a common shape.
type Agent struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"` // "ide" or "cloud"
	Agent     string    `json:"agent"`  // "cursor", "claude", ... (IDE) or "cursor" (cloud)
	Name      string    `json:"name"`
	Status    string    `json:"status"` // working | needs_you | idle
	Branch    string    `json:"branch,omitempty"`
	Repo      string    `json:"repo,omitempty"` // cwd for IDE, repo URL for cloud
	URL       string    `json:"url,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Cursor lists Cursor cloud agents and caches the result briefly, so
// several consumers polling /api/agents share one upstream refresh per TTL.
// Failures are cached too (for a shorter TTL) so consumers polling during a
// Cursor outage share one failed lookup instead of each starting a new request.
type Cursor struct {
	baseURL string
	apiKey  string
	client  *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	wait    time.Duration

	mu        sync.Mutex
	cached    []Agent
	cachedErr error
	fetched   time.Time
	repos     map[string]cloudRepo
	refresh   chan struct{}
	revision  uint64
	closed    bool
}

type cloudRepo struct {
	url     string
	fetched time.Time
}

func NewCursor(baseURL, apiKey string, options ...Options) *Cursor {
	if baseURL == "" {
		baseURL = CursorAPIBase
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cursor{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: cloudHTTPTimeout},
		ctx:     ctx,
		cancel:  cancel,
		wait:    cloudWaitTimeout,
		repos:   make(map[string]cloudRepo),
	}
	if len(options) > 0 && options[0].WaitTimeout > 0 {
		c.wait = options[0].WaitTimeout
	}
	return c
}

// enabled reports whether a Cursor API key is configured.
func (c *Cursor) Enabled() bool { return c.apiKey != "" }

// list shares one refresh across callers. A slow refresh continues after the
// bounded request wait, returning the last snapshot with an explicit error.
func (c *Cursor) List() ([]Agent, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, context.Canceled
	}
	if !c.Enabled() {
		c.mu.Unlock()
		return nil, nil
	}
	ttl := cloudCacheTTL
	if c.cachedErr != nil {
		ttl = cloudErrorTTL
	}
	if !c.fetched.IsZero() && time.Since(c.fetched) < ttl {
		defer c.mu.Unlock()
		return slices.Clone(c.cached), c.cachedErr
	}
	done := c.refresh
	if done == nil {
		done = make(chan struct{})
		c.refresh = done
		go c.fetchAndCache(done)
	}
	c.mu.Unlock()

	timer := time.NewTimer(c.wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refresh == done {
		return slices.Clone(c.cached), errors.Join(c.cachedErr, errors.New("refresh in progress; cloud results may be incomplete"))
	}
	return slices.Clone(c.cached), c.cachedErr
}

func (c *Cursor) fetchAndCache(done chan struct{}) {
	ctx, cancel := context.WithTimeout(c.ctx, cloudFetchTimeout)
	defer cancel()
	items, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cached, c.cachedErr = items, err
	c.fetched = time.Now()
	c.revision++
	c.refresh = nil
	close(done)
}

// get decodes one authenticated Cursor response within the refresh deadline.
func (c *Cursor) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.apiKey, "")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cursor api returned %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// fetchPages follows every page before filtering by status. A failed page or
// repeated cursor returns the agents already found together with an error.
func (c *Cursor) fetchPages(ctx context.Context) ([]Agent, error) {
	var items []Agent
	seenIDs := make(map[string]bool)
	seenCursors := make(map[string]bool)
	cursor := ""
	for {
		seenCursors[cursor] = true
		query := url.Values{"limit": {"100"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page struct {
			Items []struct {
				ID        string    `json:"id"`
				Name      string    `json:"name"`
				Status    string    `json:"status"`
				URL       string    `json:"url"`
				UpdatedAt time.Time `json:"updatedAt"`
				Repos     *[]struct {
					URL string `json:"url"`
				} `json:"repos"`
			} `json:"items"`
			NextCursor string `json:"nextCursor"`
		}
		if err := c.get(ctx, "/v1/agents?"+query.Encode(), &page); err != nil {
			return items, fmt.Errorf("list cursor agents: %w", err)
		}
		for _, a := range page.Items {
			// Agents can move between pages while the upstream list changes.
			if seenIDs[a.ID] {
				continue
			}
			seenIDs[a.ID] = true
			// Some Cursor deployments include repos on list items. Preserve
			// even an empty list; only an absent value needs a detail lookup.
			if a.Repos != nil {
				repo := ""
				if len(*a.Repos) > 0 {
					repo = (*a.Repos)[0].URL
				}
				c.repos[a.ID] = cloudRepo{url: repo, fetched: time.Now()}
			}
			webURL, urlErr := AgentURL(a.URL)
			if urlErr != nil {
				webURL = "https://cursor.com/agents/" + url.PathEscape(a.ID)
			}
			items = append(items, Agent{
				ID:        a.ID,
				Source:    "cloud",
				Agent:     "cursor",
				Name:      a.Name,
				Status:    cloudStatus(a.Status),
				URL:       webURL,
				UpdatedAt: a.UpdatedAt,
			})
		}
		if page.NextCursor == "" {
			return items, nil
		}
		if seenCursors[page.NextCursor] {
			return items, fmt.Errorf("cursor pagination repeated a cursor")
		}
		cursor = page.NextCursor
	}
}

// fetch fills repository metadata missing from list items. Only one refresh
// runs at a time; workers write distinct item/error slots, never the repo map.
func (c *Cursor) fetch(ctx context.Context) ([]Agent, error) {
	items, listErr := c.fetchPages(ctx)
	if listErr == nil {
		present := make(map[string]bool, len(items))
		for _, item := range items {
			present[item.ID] = true
		}
		for id := range c.repos {
			if !present[id] {
				delete(c.repos, id)
			}
		}
	}
	var pending []int
	for i := range items {
		cached, ok := c.repos[items[i].ID]
		items[i].Repo = cached.url
		if !ok || time.Since(cached.fetched) >= cloudRepoTTL {
			pending = append(pending, i)
		}
	}

	detailErrs := make([]error, len(pending))
	workers := min(cloudRepoWorkers, len(pending))
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := worker; n < len(pending); n += workers {
				i := pending[n]
				if err := ctx.Err(); err != nil {
					detailErrs[n] = err
					continue
				}
				var detail struct {
					Repos []struct {
						URL string `json:"url"`
					} `json:"repos"`
				}
				if err := c.get(ctx, "/v1/agents/"+url.PathEscape(items[i].ID), &detail); err != nil {
					detailErrs[n] = fmt.Errorf("agent %q: %w", items[i].ID, err)
					continue
				}
				items[i].Repo = ""
				if len(detail.Repos) > 0 {
					items[i].Repo = detail.Repos[0].URL
				}
			}
		}()
	}
	wg.Wait()

	var firstErr error
	failed := 0
	for n, i := range pending {
		if err := detailErrs[n]; err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// Cache empty repositories too: Cursor supports agents without a repo.
		c.repos[items[i].ID] = cloudRepo{url: items[i].Repo, fetched: time.Now()}
	}
	if failed > 0 {
		return items, errors.Join(listErr, fmt.Errorf("repository lookup failed for %d agents: %w", failed, firstErr))
	}
	return items, listErr
}

// cloudStatus maps a Cursor agent status to the shared states. ACTIVE means a
// turn is running; everything else (IDLE, FINISHED, ...) is not working.
func cloudStatus(status string) string {
	if strings.EqualFold(status, "ACTIVE") {
		return Working
	}
	return Idle
}
