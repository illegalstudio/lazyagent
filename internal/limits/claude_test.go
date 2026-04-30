package limits

import (
	"strings"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/version"
)

func TestUserAgent(t *testing.T) {
	got := userAgent()
	if !strings.HasPrefix(got, "lazyagent/") {
		t.Errorf("UA must start with %q, got %q", "lazyagent/", got)
	}
	if strings.Count(got, "lazyagent") != 2 {
		// One in "lazyagent/<ver>", one in the URL fragment. More = duplicated identity bug.
		t.Errorf("UA has unexpected lazyagent count: %q", got)
	}
	if !strings.Contains(got, version.Version) {
		t.Errorf("UA must include version %q, got %q", version.Version, got)
	}
	if !strings.Contains(got, "https://github.com/illegalstudio/lazyagent") {
		t.Errorf("UA must include project URL, got %q", got)
	}
}
