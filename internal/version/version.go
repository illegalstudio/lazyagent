package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Version and Commit are set via ldflags at build time.
var (
	Version = "dev"
	Commit  = "none"
)

// String returns a human-readable version string.
func String() string {
	return fmt.Sprintf("lazyagent v%s (%s)", Version, Commit)
}

// CheckLatest checks GitHub for a newer release. Returns the new version tag
// (e.g. "v0.7.0") if an update is available, or empty string if current or on error.
// Silently returns empty on any failure — this is a best-effort check.
func CheckLatest() string {
	if Version == "dev" {
		return ""
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/illegalstudio/lazyagent/releases/latest")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(Version, "v")
	if latest != "" && latest != current {
		return release.TagName
	}
	return ""
}
