package webhook

import (
	"strings"

	"github.com/illegalstudio/lazyagent/internal/core"
)

// Matches returns true when the event passes the webhook's event/agent filters.
// Empty filter slices match everything.
func Matches(w core.WebhookConfig, ev core.SessionEvent) bool {
	if len(w.Events) > 0 {
		want := strings.ToLower(string(ev.To))
		ok := false
		for _, e := range w.Events {
			if strings.ToLower(e) == want {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(w.Agents) > 0 {
		want := strings.ToLower(ev.Agent)
		ok := false
		for _, a := range w.Agents {
			if strings.ToLower(a) == want {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
