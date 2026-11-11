package core

import (
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/illegalstudio/lazyagent/internal/cloud"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// CloudStatus is safe for UI bindings: it contains no credential material.
type CloudStatus struct {
	Configured bool      `json:"configured"`
	Loading    bool      `json:"loading"`
	Count      int       `json:"count"`
	Error      string    `json:"error"`
	UpdatedAt  time.Time `json:"updatedAt"`
	ShowIdle   bool      `json:"showIdle"`
	Revision   uint64    `json:"-"`
}

func providerHasCursor(provider SessionProvider) bool {
	switch p := provider.(type) {
	case *CursorProvider:
		return true
	case MultiProvider:
		for _, member := range p.Providers {
			if providerHasCursor(member) {
				return true
			}
		}
	case *MultiProvider:
		for _, member := range p.Providers {
			if providerHasCursor(member) {
				return true
			}
		}
	}
	return false
}

// ConfigureCloud enables cloud discovery only for monitor providers that
// include Cursor. Demo, other-agent and maintenance providers remain local.
func (m *SessionManager) ConfigureCloud() {
	var client *cloud.Cursor
	var err error
	if providerHasCursor(m.provider) {
		var key string
		key, _, err = ResolveProviderAPIKey("cursor")
		if key != "" {
			client = cloud.NewCursor("", key)
		}
	}
	m.setCloud(client, err)
}

// CloudDisplayText removes remote terminal escape sequences and controls.
func CloudDisplayText(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(value)))
}

func (m *SessionManager) setCloud(client *cloud.Cursor, err error) {
	m.mu.Lock()
	old := m.cloud
	m.cloud = client
	m.cloudConfigErr = err
	m.cloudGeneration++
	m.cloudSessions = nil
	m.cloudState = CloudStatus{}
	m.mu.Unlock()
	if old != nil {
		old.Close()
	}
	m.UpdateCloud()
}

// UpdateCloud samples the shared client without waiting for network work.
// The immutable view map changes only when a refresh completes.
func (m *SessionManager) UpdateCloud() bool {
	m.mu.RLock()
	client, configErr, generation := m.cloud, m.cloudConfigErr, m.cloudGeneration
	m.mu.RUnlock()
	status := CloudStatus{}
	var items []cloud.Agent
	if client != nil {
		var state cloud.State
		items, state = client.Snapshot()
		status = CloudStatus{Configured: state.Configured, Loading: state.Loading, Count: state.Count, Error: state.Error, UpdatedAt: state.UpdatedAt, Revision: state.Revision}
	} else if configErr != nil {
		status.Configured = true
		status.Error = configErr.Error()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if generation != m.cloudGeneration {
		return false
	}
	if status.Revision < m.cloudState.Revision {
		return false
	}
	status.ShowIdle = m.cloudShowIdle
	changed := status != m.cloudState
	if status.Revision != m.cloudState.Revision {
		view := make(map[string]*model.Session, len(items))
		for _, item := range items {
			id := "cloud:cursor:" + item.ID
			webURL, urlErr := cloud.AgentURL(item.URL)
			if urlErr != nil {
				webURL = "https://cursor.com/agents/" + url.PathEscape(item.ID)
			}
			state := model.StatusIdle
			if item.Status == cloud.Working {
				state = model.StatusThinking
			}
			name := item.Name
			if strings.TrimSpace(name) == "" {
				name = item.ID
			}
			view[id] = &model.Session{SessionID: id, Agent: "cursor", Name: CloudDisplayText(name), Status: state, LastActivity: item.UpdatedAt, GitBranch: item.Branch, Cloud: &model.CloudSession{ID: item.ID, URL: webURL, Repo: item.Repo, Status: item.Status}}
		}
		m.cloudSessions = view
	}
	m.cloudState = status
	return changed
}

func cloudActivity(session *model.Session) ActivityKind {
	if session.Cloud.Status == cloud.Working {
		return ActivityRunning
	}
	if session.Cloud.Status == cloud.NeedsYou {
		return ActivityWaiting
	}
	return ActivityIdle
}

func (m *SessionManager) CloudStatus() CloudStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cloudState
}

// RefreshCloud requests an immediate refresh without blocking the caller.
func (m *SessionManager) RefreshCloud() {
	m.mu.RLock()
	client := m.cloud
	m.mu.RUnlock()
	if client != nil {
		client.Refresh()
	}
	m.UpdateCloud()
}

func (m *SessionManager) SetCloudShowIdle(show bool) {
	m.mu.Lock()
	m.cloudShowIdle = show
	m.cloudState.ShowIdle = show
	m.mu.Unlock()
}
