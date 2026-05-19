package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
)

func TestPayload_MarshalContainsExpectedFields(t *testing.T) {
	p := Payload{
		ID:          "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Event:       "state_transition",
		SessionID:   "abc",
		Agent:       "claude",
		From:        string(core.ActivityIdle),
		To:          string(core.ActivityWaiting),
		ProjectPath: "/p",
		Timestamp:   time.Date(2026, 5, 19, 14, 30, 0, 0, time.UTC),
		API: &APILinks{
			SessionURL: "http://127.0.0.1:7421/api/sessions/abc",
			DetailURL:  "http://127.0.0.1:7421/api/sessions/abc/full",
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"id":"f47ac10b`, `"event":"state_transition"`, `"session_id":"abc"`,
		`"agent":"claude"`, `"from":"idle"`, `"to":"waiting"`,
		`"project_path":"/p"`, `"timestamp":"2026-05-19T14:30:00Z"`,
		`"api":{`, `"session_url":"http://127.0.0.1:7421/api/sessions/abc"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestPayload_MarshalOmitsAPIWhenNil(t *testing.T) {
	p := Payload{ID: "x", Event: "state_transition", SessionID: "s"}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), `"api"`) {
		t.Fatalf("api field should be omitted: %s", b)
	}
}
