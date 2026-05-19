package webhook

import (
	"testing"

	"github.com/illegalstudio/lazyagent/internal/core"
)

func TestMatches(t *testing.T) {
	ev := core.SessionEvent{Agent: "claude", To: core.ActivityWaiting}

	cases := []struct {
		name    string
		w       core.WebhookConfig
		matches bool
	}{
		{"empty filters match all", core.WebhookConfig{Name: "x", URL: "https://x"}, true},
		{"matching event", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"waiting"}}, true},
		{"non-matching event", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"thinking"}}, false},
		{"matching agent", core.WebhookConfig{Name: "x", URL: "https://x", Agents: []string{"claude"}}, true},
		{"non-matching agent", core.WebhookConfig{Name: "x", URL: "https://x", Agents: []string{"codex"}}, false},
		{"event AND agent both match", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"waiting"}, Agents: []string{"claude"}}, true},
		{"event matches, agent doesn't", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"waiting"}, Agents: []string{"codex"}}, false},
		{"case-insensitive event", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"WAITING"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Matches(c.w, ev); got != c.matches {
				t.Fatalf("got %v, want %v", got, c.matches)
			}
		})
	}
}
