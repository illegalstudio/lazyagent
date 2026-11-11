package credentials

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/core"
)

func runCommand(t *testing.T, args []string, input string) (int, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var out bytes.Buffer
	code := run(args, in, &out, &out)
	return code, out.String()
}

func TestCredentialCommandsNeverPrintKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	const saved = "saved-test-credential-do-not-print"
	if code, out := runCommand(t, []string{"set", "cursor"}, saved+"\n"); code != 0 || strings.Contains(out, saved) {
		t.Fatalf("set code=%d, key exposed=%t", code, strings.Contains(out, saved))
	}
	if key, _, err := core.ResolveProviderAPIKey("cursor"); err != nil || key != saved {
		t.Fatalf("piped key was not saved: %v", err)
	}
	if code, out := runCommand(t, []string{"status", "cursor"}, ""); code != 0 || !strings.Contains(out, core.CredentialsPath()) || strings.Contains(out, saved) {
		t.Fatalf("status code=%d, expected saved source without key", code)
	}
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "environment-test-credential-do-not-print")
	if code, out := runCommand(t, []string{"status", "cursor"}, ""); code != 0 || !strings.Contains(out, "LAZYAGENT_CURSOR_API_KEY") || strings.Contains(out, "environment-test-credential") {
		t.Fatalf("status code=%d, expected environment source without key", code)
	}
	if code, out := runCommand(t, []string{"remove", "cursor"}, ""); code != 0 || !strings.Contains(out, "still overrides") || strings.Contains(out, saved) {
		t.Fatalf("remove code=%d, expected environment override notice", code)
	}
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	if code, out := runCommand(t, []string{"status", "cursor"}, ""); code != 0 || !strings.Contains(out, "not configured") {
		t.Fatalf("removed key is still configured: code=%d", code)
	}
}

func TestCredentialSetRejectsInvalidInputWithoutLeakingIt(t *testing.T) {
	for _, input := range []string{"", "first\nsecond", strings.Repeat("x", 65536) + "truncated-secret"} {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		code, out := runCommand(t, []string{"set", "cursor"}, input)
		if code == 0 || strings.Contains(out, "truncated-secret") || strings.Contains(out, "first\nsecond") {
			t.Fatalf("invalid input was accepted or printed: code=%d", code)
		}
		if _, err := os.Stat(core.CredentialsPath()); !os.IsNotExist(err) {
			t.Fatalf("invalid input created credentials: %v", err)
		}
	}
}

func TestCredentialCommandsUsageAndStatusDoNotWriteConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("LAZYAGENT_CURSOR_API_KEY", "")
	for _, tc := range []struct {
		args []string
		code int
	}{
		{nil, 0},
		{[]string{"--help"}, 0},
		{[]string{"status", "cursor"}, 0},
		{[]string{"unknown", "cursor"}, 2},
		{[]string{"set", "cursor", "do-not-print-this-argument"}, 2},
		{[]string{"set", "../cursor"}, 2},
	} {
		code, out := runCommand(t, tc.args, "")
		if code != tc.code || strings.Contains(out, "do-not-print-this-argument") {
			t.Fatalf("command code=%d, want %d", code, tc.code)
		}
	}
	if _, err := os.Stat(core.ConfigDir()); !os.IsNotExist(err) {
		t.Fatalf("help/status created config: %v", err)
	}
}
