package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
	if latest != "" && isNewer(latest, current) {
		return release.TagName
	}
	return ""
}

// isNewer returns true if version a is strictly newer than b (semver major.minor.patch).
func isNewer(a, b string) bool {
	ap := parseSemver(a)
	bp := parseSemver(b)
	if ap == nil || bp == nil {
		return false // can't compare, assume up-to-date
	}
	for i := 0; i < 3; i++ {
		if ap[i] > bp[i] {
			return true
		}
		if ap[i] < bp[i] {
			return false
		}
	}
	return false
}

// parseSemver extracts [major, minor, patch] from a version string like "1.2.3".
// Returns nil if parsing fails.
func parseSemver(v string) []int {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	return nums
}
