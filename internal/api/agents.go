package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
)

const (
	// cursorKeyEnv overrides the shared saved Cursor API key.
	cursorKeyEnv      = "LAZYAGENT_CURSOR_API_KEY"
	cursorAPIBase     = "https://api.cursor.com"
	cloudCacheTTL     = 30 * time.Second
	cloudErrorTTL     = 5 * time.Second
	cloudFetchTimeout = 2 * time.Minute
	cloudWaitTimeout  = 10 * time.Second
	cloudHTTPTimeout  = 30 * time.Second
	cloudRepoTTL      = 5 * time.Minute
	cloudRepoWorkers  = 4
)

// Normalized agent states shared by IDE and cloud agents.
const (
	agentWorking  = "working"
	agentNeedsYou = "needs_you"
	agentIdle     = "idle"
)

// AgentItem is one coding agent, IDE chat or Cursor cloud agent, in a common shape.
type AgentItem struct {
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

// AgentsResponse is returned by GET /api/agents. Errors lists sources that
// failed, so a consumer can tell "no agents" from "cloud lookup broke".
type AgentsResponse struct {
	Agents []AgentItem `json:"agents"`
	Errors []string    `json:"errors"`
}

// cloudAgents lists Cursor cloud agents and caches the result briefly, so
// several consumers polling /api/agents share one upstream refresh per TTL.
// Failures are cached too (for a shorter TTL) so consumers polling during a
// Cursor outage share one failed lookup instead of each starting a new request.
type cloudAgents struct {
	baseURL string
	apiKey  string
	client  *http.Client
	ctx     context.Context
	cancel  context.CancelFunc
	wait    time.Duration

	mu        sync.Mutex
	cached    []AgentItem
	cachedErr error
	fetched   time.Time
	repos     map[string]cloudRepo
	refresh   chan struct{}
}

type cloudRepo struct {
	url     string
	fetched time.Time
}

func newCloudAgents(baseURL, apiKey string) *cloudAgents {
	ctx, cancel := context.WithCancel(context.Background())
	return &cloudAgents{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: cloudHTTPTimeout},
		ctx:     ctx,
		cancel:  cancel,
		wait:    cloudWaitTimeout,
		repos:   make(map[string]cloudRepo),
	}
}

// enabled reports whether a Cursor API key is configured.
func (c *cloudAgents) enabled() bool { return c.apiKey != "" }

// list shares one refresh across callers. A slow refresh continues after the
// bounded request wait, returning the last snapshot with an explicit error.
func (c *cloudAgents) list() ([]AgentItem, error) {
	c.mu.Lock()
	ttl := cloudCacheTTL
	if c.cachedErr != nil {
		ttl = cloudErrorTTL
	}
	if !c.fetched.IsZero() && time.Since(c.fetched) < ttl {
		defer c.mu.Unlock()
		return c.cached, c.cachedErr
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
		return c.cached, errors.Join(c.cachedErr, errors.New("refresh in progress; cloud results may be incomplete"))
	}
	return c.cached, c.cachedErr
}

func (c *cloudAgents) fetchAndCache(done chan struct{}) {
	ctx, cancel := context.WithTimeout(c.ctx, cloudFetchTimeout)
	defer cancel()
	items, err := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cached, c.cachedErr = items, err
	c.fetched = time.Now()
	c.refresh = nil
	close(done)
}

// get decodes one authenticated Cursor response within the refresh deadline.
func (c *cloudAgents) get(ctx context.Context, path string, out any) error {
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
func (c *cloudAgents) fetchPages(ctx context.Context) ([]AgentItem, error) {
	var items []AgentItem
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
			items = append(items, AgentItem{
				ID:        a.ID,
				Source:    "cloud",
				Agent:     "cursor",
				Name:      a.Name,
				Status:    cloudStatus(a.Status),
				URL:       a.URL,
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
func (c *cloudAgents) fetch(ctx context.Context) ([]AgentItem, error) {
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
		return agentWorking
	}
	return agentIdle
}

// ideStatus maps a lazyagent activity to the shared states, matching the
// split Helm uses: waiting = needs_you, idle = idle, anything else = working.
func ideStatus(activity core.ActivityKind) string {
	switch activity {
	case core.ActivityWaiting:
		return agentNeedsYou
	case core.ActivityIdle:
		return agentIdle
	default:
		return agentWorking
	}
}

// ideAgents returns every visible local session as an AgentItem.
func (s *Server) ideAgents() []AgentItem {
	visible := s.manager.QuerySessions("", "")
	items := make([]AgentItem, 0, len(visible))
	for _, sess := range visible {
		name := s.manager.SessionName(sess.SessionID)
		if name == "" {
			name = sess.Name
		}
		if name == "" {
			name = core.ShortName(sess.CWD, 60)
		}
		items = append(items, AgentItem{
			ID:        sess.SessionID,
			Source:    "ide",
			Agent:     sess.Agent,
			Name:      name,
			Status:    ideStatus(s.manager.ActivityFor(sess.SessionID)),
			Branch:    sess.GitBranch,
			Repo:      sess.CWD,
			UpdatedAt: sess.LastActivity,
		})
	}
	return items
}

// handleGetAgents serves GET /api/agents: IDE chats plus Cursor cloud agents
// in one list. Idle agents are left out unless ?all=true.
func (s *Server) handleGetAgents(w http.ResponseWriter, r *http.Request) {
	includeIdle := r.URL.Query().Get("all") == "true"
	resp := AgentsResponse{Agents: []AgentItem{}, Errors: []string{}}

	all := s.ideAgents()
	if s.cloudConfigErr != nil {
		resp.Errors = append(resp.Errors, "cloud: "+s.cloudConfigErr.Error())
	} else if s.cloud.enabled() {
		cloud, err := s.cloud.list()
		if err != nil {
			resp.Errors = append(resp.Errors, "cloud: "+err.Error())
		}
		all = append(all, cloud...)
	}

	for _, a := range all {
		if includeIdle || a.Status != agentIdle {
			resp.Agents = append(resp.Agents, a)
		}
	}
	sort.SliceStable(resp.Agents, func(i, j int) bool {
		return resp.Agents[i].UpdatedAt.After(resp.Agents[j].UpdatedAt)
	})
	writeJSON(w, http.StatusOK, resp)
}
