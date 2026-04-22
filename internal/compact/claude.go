package compact

import "fmt"

// compactClaudeLine rewrites a single Claude Code JSONL entry, truncating
// oversized payloads while keeping the message graph intact so the session
// stays resumable with `claude --resume`.
//
// Field paths targeted (observed in real ~/.claude/projects/* transcripts):
//   - toolUseResult.stdout            — bash output
//   - toolUseResult.originalFile      — pre-edit file snapshots
//   - toolUseResult.file.content      — Read tool results
//   - toolUseResult.content[].text    — structured tool payloads
//   - message.content[].thinking      — extended-thinking blocks
//   - message.content[].text          — rare huge assistant/user text
//   - message.content[].content       — nested content strings
//   - message.content[].source.data   — base64-encoded images
//
// Thinking blocks get a larger budget (2× threshold) since they're genuine
// model reasoning, not incidental I/O.
func compactClaudeLine(entry map[string]any, threshold int64) int64 {
	var saved int64

	if tur, ok := entry["toolUseResult"].(map[string]any); ok {
		saved += compactClaudeToolResult(tur, threshold)
	}

	if msg, ok := entry["message"].(map[string]any); ok {
		saved += compactClaudeMessage(msg, threshold)
	}

	// Some progress-type entries wrap a message under `data.message.message`.
	if data, ok := entry["data"].(map[string]any); ok {
		if nested, ok := data["message"].(map[string]any); ok {
			if inner, ok := nested["message"].(map[string]any); ok {
				saved += compactClaudeMessage(inner, threshold)
			}
		}
	}

	return saved
}

func compactClaudeToolResult(tur map[string]any, threshold int64) int64 {
	var saved int64
	// Scalar tool-result fields that commonly grow into megabytes.
	for _, key := range []string{"stdout", "stderr", "originalFile"} {
		if s, ok := tur[key].(string); ok {
			if new, delta := truncateString(s, threshold); delta > 0 {
				tur[key] = new
				saved += delta
			}
		}
	}
	// `file.content` (Read tool) — treat identically.
	if file, ok := tur["file"].(map[string]any); ok {
		if s, ok := file["content"].(string); ok {
			if new, delta := truncateString(s, threshold); delta > 0 {
				file["content"] = new
				saved += delta
			}
		}
	}
	// `content[]` — array of blocks; only the text / string forms need
	// attention.
	if arr, ok := tur["content"].([]any); ok {
		saved += compactContentArray(arr, threshold)
	}
	return saved
}

func compactClaudeMessage(msg map[string]any, threshold int64) int64 {
	var saved int64
	arr, ok := msg["content"].([]any)
	if !ok {
		return saved
	}
	saved += compactContentArray(arr, threshold)

	// Thinking blocks get a more generous budget — they carry genuine model
	// reasoning that we want to keep as readable as possible.
	thinkingBudget := threshold * 2
	for _, block := range arr {
		bm, ok := block.(map[string]any)
		if !ok {
			continue
		}
		if s, ok := bm["thinking"].(string); ok {
			if new, delta := truncateString(s, thinkingBudget); delta > 0 {
				bm["thinking"] = new
				saved += delta
			}
		}
	}
	return saved
}

// compactContentArray walks a message `content` array and truncates any
// oversized string leaves (text, content, source.data). The array is
// mutated in place.
func compactContentArray(arr []any, threshold int64) int64 {
	var saved int64
	for _, block := range arr {
		bm, ok := block.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"text", "content"} {
			if s, ok := bm[key].(string); ok {
				if new, delta := truncateString(s, threshold); delta > 0 {
					bm[key] = new
					saved += delta
				}
			}
		}
		// Images: replace base64 payload with a marker but keep the
		// envelope so tools that expect a source block don't choke.
		if src, ok := bm["source"].(map[string]any); ok {
			if s, ok := src["data"].(string); ok && int64(len(s)) > threshold {
				src["data"] = fmt.Sprintf("[image stripped by lazyagent compact — was %d bytes]", len(s))
				saved += int64(len(s)) - int64(len(src["data"].(string)))
			}
		}
	}
	return saved
}

// truncateString returns (new, savedBytes). If s is already within the
// threshold or not worth truncating, new == s and savedBytes == 0.
// The truncated value keeps the first threshold/10 characters so log
// snippets and error headers stay readable.
func truncateString(s string, threshold int64) (string, int64) {
	n := int64(len(s))
	if n <= threshold {
		return s, 0
	}
	keep := threshold / 10
	if keep < 256 {
		keep = 256
	}
	if keep >= n {
		return s, 0
	}
	head := s[:keep]
	marker := fmt.Sprintf("\n\n[truncated by lazyagent compact — was %d bytes, kept first %d]", n, keep)
	new := head + marker
	return new, n - int64(len(new))
}
