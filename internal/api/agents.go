package api

import (
	"github.com/illegalstudio/lazyagent/internal/cloud"
	"github.com/illegalstudio/lazyagent/internal/core"
	"net/http"
	"sort"
	"time"
)

const (
	cursorKeyEnv  = "LAZYAGENT_CURSOR_API_KEY"
	cursorAPIBase = cloud.CursorAPIBase
	agentWorking  = cloud.Working
	agentNeedsYou = cloud.NeedsYou
	agentIdle     = cloud.Idle
)

type AgentItem = cloud.Agent

type AgentsResponse struct {
	Agents []AgentItem `json:"agents"`
	Errors []string    `json:"errors"`
}

func newCloudAgents(baseURL, apiKey string, wait ...time.Duration) *cloud.Cursor {
	options := cloud.Options{}
	if len(wait) > 0 {
		options.WaitTimeout = wait[0]
	}
	return cloud.NewCursor(baseURL, apiKey, options)
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
	} else if s.cloud.Enabled() {
		cloud, err := s.cloud.List()
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
