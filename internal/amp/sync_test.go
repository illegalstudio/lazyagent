package amp

import "testing"

func TestParseListLine(t *testing.T) {
	tests := []struct {
		line    string
		wantID  string
		wantMsg int
	}{
		{
			"Next roadmap steps for project                5m ago        Private     4         T-019d4898-48e5-765d-9698-61133acf4825",
			"T-019d4898-48e5-765d-9698-61133acf4825", 4,
		},
		{
			"Project status update                         14h ago       Private     33        T-019d44a3-c374-745a-ae8a-621d7eb3a90b",
			"T-019d44a3-c374-745a-ae8a-621d7eb3a90b", 33,
		},
		{
			"Untitled                                      2d ago        Private     1         T-019d3e52-04cd-739c-a583-8950c1ca2d2a",
			"T-019d3e52-04cd-739c-a583-8950c1ca2d2a", 1,
		},
		{"", "", 0},
		{"some garbage", "", 0},
	}

	for _, tt := range tests {
		id, msgs := parseListLine(tt.line)
		if id != tt.wantID || msgs != tt.wantMsg {
			t.Errorf("parseListLine(%q) = (%q, %d), want (%q, %d)", tt.line, id, msgs, tt.wantID, tt.wantMsg)
		}
	}
}

func TestListRemoteThreadsParsing(t *testing.T) {
	// Test that the full parser works end-to-end with header + separator + data
	output := `Title                                         Last Updated  Visibility  Messages  Thread ID
────────────────────────────────────────────  ────────────  ──────────  ────────  ──────────────────────────────────────
Next roadmap steps                            5m ago        Private     4         T-aaa
Old thread                                    2d ago        Private     10        T-bbb
`
	// We can't call listRemoteThreads directly (it runs a command),
	// but we can verify parseListLine handles the real format.
	lines := []string{
		"Next roadmap steps                            5m ago        Private     4         T-aaa",
		"Old thread                                    2d ago        Private     10        T-bbb",
	}
	_ = output // used for documentation

	for i, line := range lines {
		id, _ := parseListLine(line)
		if id == "" {
			t.Errorf("line[%d]: failed to parse thread ID", i)
		}
	}
}
