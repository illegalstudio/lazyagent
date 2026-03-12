package demo

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// Provider implements core.SessionProvider with generated fake data.
type Provider struct{}

func (Provider) DiscoverSessions() ([]*model.Session, error) {
	return GenerateSessions(), nil
}

func (Provider) UseWatcher() bool               { return false }
func (Provider) RefreshInterval() time.Duration { return 30 * time.Second }
func (Provider) WatchDirs() []string            { return nil }

// project templates for realistic-looking demo sessions.
var projects = []struct {
	CWD       string
	GitBranch string
	Model     string
	Agent     string // "claude" or "pi"
}{
	{"/Users/dev/projects/webapp-frontend", "feature/dashboard-redesign", "claude-sonnet-4-5-20250514", "claude"},
	{"/Users/dev/projects/api-server", "fix/auth-middleware", "claude-sonnet-4-5-20250514", "claude"},
	{"/Users/dev/projects/mobile-app", "main", "claude-sonnet-4-5-20250514", "claude"},
	{"/Users/dev/projects/cli-tool", "feature/config-refactor", "claude-opus-4-6-20250610", "claude"},
	{"/Users/dev/projects/data-pipeline", "feature/streaming-etl", "gemini-3-pro", "pi"},
	{"/Users/dev/projects/infra-terraform", "chore/upgrade-providers", "claude-sonnet-4-5-20250514", "pi"},
	{"/Users/dev/projects/docs-site", "feature/api-reference", "claude-haiku-4-5-20251001", "claude"},
	{"/Users/dev/projects/shared-lib", "fix/race-condition", "gpt-4.1", "pi"},
}

var toolNames = []string{"Read", "Edit", "Write", "Bash", "Grep", "Glob", "Agent", "WebSearch"}

var userMessages = []string{
	"Can you refactor the authentication middleware to use JWT tokens?",
	"Fix the failing test in user_service_test.go",
	"Add pagination support to the /api/items endpoint",
	"Update the README with the new installation steps",
	"There's a race condition in the cache layer, can you investigate?",
	"Implement the dark mode toggle for the settings page",
	"The CI pipeline is broken, looks like a dependency issue",
	"Add error handling for the database connection timeout",
	"Can you optimize the SQL query in the reports module?",
	"Write unit tests for the new validation helpers",
}

var assistantMessages = []string{
	"I'll start by reading the current middleware implementation to understand the structure.",
	"I found the issue — the test was relying on a hardcoded timestamp. Let me fix that.",
	"I've added cursor-based pagination with a default page size of 25. Here's what changed:",
	"The race condition is in the cache invalidation logic. Two goroutines can write simultaneously.",
	"I've refactored the query to use a CTE which should reduce execution time significantly.",
	"Let me search for all the files that need to be updated for dark mode support.",
	"The dependency conflict is between v2.3.0 and v2.4.1 of the HTTP client library.",
	"I'll write comprehensive tests covering edge cases for empty input, overflow, and unicode.",
	"Done! I've updated the README with the new Docker-based installation flow.",
	"I've implemented the error handling with exponential backoff and a circuit breaker pattern.",
}

var fileWrites = []string{
	"src/middleware/auth.ts",
	"internal/service/user_test.go",
	"api/handlers/items.go",
	"README.md",
	"pkg/cache/store.go",
	"src/components/Settings.tsx",
	"Dockerfile",
	"internal/db/connection.go",
	"sql/reports/monthly.sql",
	"tests/validation_test.py",
}

// statusOption pairs a JSONL status with an optional current tool.
type statusOption struct {
	status model.SessionStatus
	tool   string
}

// activeStatuses are states that represent ongoing work. Sessions randomly
// rotate through these so the demo always shows multiple busy agents.
var activeStatuses = []statusOption{
	{model.StatusExecutingTool, "Edit"},
	{model.StatusExecutingTool, "Write"},
	{model.StatusExecutingTool, "Bash"},
	{model.StatusExecutingTool, "Read"},
	{model.StatusExecutingTool, "Grep"},
	{model.StatusExecutingTool, "Glob"},
	{model.StatusExecutingTool, "Agent"},
	{model.StatusExecutingTool, "WebSearch"},
	{model.StatusThinking, ""},
	{model.StatusProcessingResult, ""},
}

// GenerateSessions creates a set of realistic fake sessions for demo purposes.
// Each call randomises statuses and timestamps so that multiple sessions appear
// actively working at the same time, making the UI look alive.
func GenerateSessions() []*model.Session {
	now := time.Now()
	sessions := make([]*model.Session, len(projects))

	for i, p := range projects {
		// Decide whether this session is currently active.
		// ~70% of sessions are active, the rest are waiting or idle.
		var status model.SessionStatus
		var currentTool string
		var lastActivity time.Time

		roll := rand.Intn(100)
		switch {
		case roll < 55:
			// Active — doing something right now.
			pick := activeStatuses[rand.Intn(len(activeStatuses))]
			status = pick.status
			currentTool = pick.tool
			// Last activity is very recent (0-8s ago) so ResolveActivity keeps it active.
			lastActivity = now.Add(-time.Duration(rand.Intn(8)) * time.Second)
		case roll < 75:
			// Waiting for user input (recent enough to show "waiting", not "idle").
			status = model.StatusWaitingForUser
			lastActivity = now.Add(-time.Duration(15+rand.Intn(45)) * time.Second)
		default:
			// Idle — hasn't been touched in a while.
			status = model.StatusWaitingForUser
			lastActivity = now.Add(-time.Duration(3+rand.Intn(10)) * time.Minute)
		}

		nUser := 5 + rand.Intn(20)
		nAssistant := nUser + rand.Intn(5)
		total := nUser + nAssistant

		inputTokens := 50000 + rand.Intn(200000)
		outputTokens := 10000 + rand.Intn(80000)
		cacheCreation := rand.Intn(30000)
		cacheRead := rand.Intn(100000)

		// Build recent tools — the last tool must be very recent for active sessions
		// so ResolveActivity picks it up.
		nTools := 5 + rand.Intn(16)
		recentTools := make([]model.ToolCall, nTools)
		for j := range recentTools {
			toolAge := time.Duration(nTools-j) * 15 * time.Second
			recentTools[j] = model.ToolCall{
				Name:      toolNames[rand.Intn(len(toolNames))],
				Timestamp: lastActivity.Add(-toolAge),
			}
		}
		if currentTool != "" && len(recentTools) > 0 {
			recentTools[len(recentTools)-1] = model.ToolCall{
				Name:      currentTool,
				Timestamp: lastActivity,
			}
		}

		// Build recent messages
		nMsgs := 6 + rand.Intn(5)
		if nMsgs > 10 {
			nMsgs = 10
		}
		recentMsgs := make([]model.ConversationMessage, nMsgs)
		for j := range recentMsgs {
			msgAge := time.Duration(nMsgs-j) * 45 * time.Second
			if j%2 == 0 {
				recentMsgs[j] = model.ConversationMessage{
					Role:      "user",
					Text:      userMessages[rand.Intn(len(userMessages))],
					Timestamp: lastActivity.Add(-msgAge),
				}
			} else {
				recentMsgs[j] = model.ConversationMessage{
					Role:      "assistant",
					Text:      assistantMessages[rand.Intn(len(assistantMessages))],
					Timestamp: lastActivity.Add(-msgAge),
				}
			}
		}

		// Build entry timestamps for sparkline — cluster them to create
		// interesting patterns with bursts of activity.
		nEntries := 50 + rand.Intn(150)
		entryTimestamps := make([]time.Time, nEntries)
		window := 30 * time.Minute
		// Create 2-4 burst centres within the window.
		nBursts := 2 + rand.Intn(3)
		burstCentres := make([]time.Duration, nBursts)
		for b := range burstCentres {
			burstCentres[b] = time.Duration(rand.Intn(int(window.Seconds()))) * time.Second
		}
		for j := range entryTimestamps {
			// Pick a random burst centre and scatter around it.
			centre := burstCentres[rand.Intn(len(burstCentres))]
			spread := time.Duration(rand.Intn(120)) * time.Second
			offset := centre + spread
			if offset > window {
				offset = window - time.Duration(rand.Intn(60))*time.Second
			}
			entryTimestamps[j] = now.Add(-offset)
		}

		sessions[i] = &model.Session{
			SessionID:           fmt.Sprintf("demo-%04d-abcd-efgh-%04d", 1000+i, 5000+i*111),
			JSONLPath:           fmt.Sprintf("/Users/dev/.claude/projects/demo/%d.jsonl", i),
			CWD:                 p.CWD,
			Version:             "1.0.33",
			Model:               p.Model,
			GitBranch:           p.GitBranch,
			Agent:               p.Agent,
			Status:              status,
			CurrentTool:         currentTool,
			LastActivity:        lastActivity,
			TotalMessages:       total,
			UserMessages:        nUser,
			AssistantMessages:   nAssistant,
			RecentTools:         recentTools,
			RecentMessages:      recentMsgs,
			EntryTimestamps:     entryTimestamps,
			InputTokens:         inputTokens,
			OutputTokens:        outputTokens,
			CacheCreationTokens: cacheCreation,
			CacheReadTokens:     cacheRead,
			CostUSD:             0, // let the estimator compute it
			LastFileWrite:       p.CWD + "/" + fileWrites[i%len(fileWrites)],
			LastFileWriteAt:     lastActivity.Add(-time.Duration(rand.Intn(10)) * time.Second),
			IsWorktree:          i == 3,
			MainRepo:            "",
		}
		if sessions[i].IsWorktree {
			sessions[i].MainRepo = "/Users/dev/projects/cli-tool"
		}
	}

	return sessions
}
