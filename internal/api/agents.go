package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
)

const (
	// cursorKeyEnv holds the Cursor API key used to list cloud agents.
	// Unset disables the cloud half of /api/agents.
	cursorKeyEnv      = "LAZYAGENT_CURSOR_API_KEY"
	cursorAPIBase     = "https://api.cursor.com"
	cloudCacheTTL     = 30 * time.Second
	cloudFetchTimeout = 10 * time.Second
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
// several consumers polling /api/agents make one upstream call per TTL.
type cloudAgents struct {
	baseURL string
	apiKey  string
	client  *http.Client

	mu      sync.Mutex
	cached  []AgentItem
	fetched time.Time
}

func newCloudAgents(baseURL, apiKey string) *cloudAgents {
	return &cloudAgents{baseURL: baseURL, apiKey: apiKey, client: &http.Client{Timeout: cloudFetchTimeout}}
}

// enabled reports whether a Cursor API key is configured.
func (c *cloudAgents) enabled() bool { return c.apiKey != "" }

// list returns all cloud agents, newest first, from cache when fresh.
func (c *cloudAgents) list() ([]AgentItem, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetched.IsZero() && time.Since(c.fetched) < cloudCacheTTL {
		return c.cached, nil
	}

	req, err := http.NewRequest("GET", c.baseURL+"/v1/agents?limit=100", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.apiKey, "")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cursor api returned %d", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			ID        string    `json:"id"`
			Name      string    `json:"name"`
			Status    string    `json:"status"`
			URL       string    `json:"url"`
			UpdatedAt time.Time `json:"updatedAt"`
			Repos     []struct {
				URL string `json:"url"`
			} `json:"repos"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	items := make([]AgentItem, 0, len(body.Items))
	for _, a := range body.Items {
		item := AgentItem{
			ID:        a.ID,
			Source:    "cloud",
			Agent:     "cursor",
			Name:      a.Name,
			Status:    cloudStatus(a.Status),
			URL:       a.URL,
			UpdatedAt: a.UpdatedAt,
		}
		if len(a.Repos) > 0 {
			item.Repo = a.Repos[0].URL
		}
		items = append(items, item)
	}
	c.cached, c.fetched = items, time.Now()
	return items, nil
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
	if s.cloud.enabled() {
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

// cursorKeyFromEnv returns the configured Cursor API key, trimmed.
func cursorKeyFromEnv() string { return strings.TrimSpace(os.Getenv(cursorKeyEnv)) }
