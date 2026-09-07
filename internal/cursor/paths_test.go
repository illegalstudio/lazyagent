package cursor

import (
	"path/filepath"
	"testing"
)

func TestStateDBPathFor(t *testing.T) {
	tail := filepath.Join("Cursor", "User", "globalStorage", "state.vscdb")
	cases := []struct {
		name          string
		goos          string
		home          string
		xdgConfigHome string
		appData       string
		want          string
	}{
		{"darwin", "darwin", "/Users/me", "", "", filepath.Join("/Users/me", "Library", "Application Support", tail)},
		{"darwin ignores XDG", "darwin", "/Users/me", "/xdg", "", filepath.Join("/Users/me", "Library", "Application Support", tail)},
		{"linux default", "linux", "/home/me", "", "", filepath.Join("/home/me", ".config", tail)},
		{"linux XDG_CONFIG_HOME", "linux", "/home/me", "/home/me/cfg", "", filepath.Join("/home/me/cfg", tail)},
		{"freebsd follows linux", "freebsd", "/home/me", "", "", filepath.Join("/home/me", ".config", tail)},
		{"windows", "windows", `C:\Users\me`, "", `C:\Users\me\AppData\Roaming`, filepath.Join(`C:\Users\me\AppData\Roaming`, tail)},
		{"windows without APPDATA", "windows", `C:\Users\me`, "", "", ""},
		{"no home", "linux", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stateDBPathFor(tc.goos, tc.home, tc.xdgConfigHome, tc.appData); got != tc.want {
				t.Errorf("stateDBPathFor(%q) = %q, want %q", tc.goos, got, tc.want)
			}
		})
	}
}

func TestStateDBDirIsParentOfPath(t *testing.T) {
	p := stateDBPath()
	if p == "" {
		t.Skip("home directory not resolvable")
	}
	if got, want := StateDBDir(), filepath.Dir(p); got != want {
		t.Errorf("StateDBDir() = %q, want %q", got, want)
	}
}
