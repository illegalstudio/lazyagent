package compact

// compactPiLine rewrites a single pi coding-agent JSONL entry.
//
// Field paths targeted (observed in real ~/.pi/agent/sessions/* transcripts):
//   - message.content[].text                    — text blocks
//   - message.content[].thinking                — thinking blocks (2× threshold)
//   - message.content[].thinkingSignature       — crypto attestation blob
//   - message.content[].arguments               — toolCall argument JSON
//   - message.details.truncation.content        — post-compaction snapshots
//   - summary                                   — compaction summary (only if huge)
//
// Pi already externalises very large tool outputs via
// `message.details.fullOutputPath`, so we don't see raw stdout inline as
// often as on Claude. The structure is otherwise Claude-like.
func compactPiLine(entry map[string]any, threshold int64) int64 {
	var saved int64

	if msg, ok := entry["message"].(map[string]any); ok {
		saved += compactPiMessage(msg, threshold)
	}

	// Summaries are usually short (<20KB) and semantically useful, so we
	// only touch them if they blow past a generous ceiling.
	if s, ok := entry["summary"].(string); ok {
		if new, delta := truncateString(s, threshold*4); delta > 0 {
			entry["summary"] = new
			saved += delta
		}
	}
	return saved
}

func compactPiMessage(msg map[string]any, threshold int64) int64 {
	var saved int64
	thinkingBudget := threshold * 2

	if arr, ok := msg["content"].([]any); ok {
		for _, block := range arr {
			bm, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := bm["text"].(string); ok {
				if new, delta := truncateString(s, threshold); delta > 0 {
					bm["text"] = new
					saved += delta
				}
			}
			if s, ok := bm["thinking"].(string); ok {
				if new, delta := truncateString(s, thinkingBudget); delta > 0 {
					bm["thinking"] = new
					saved += delta
				}
			}
			if s, ok := bm["thinkingSignature"].(string); ok {
				if new, delta := truncateString(s, threshold); delta > 0 {
					bm["thinkingSignature"] = new
					saved += delta
				}
			}
			if s, ok := bm["arguments"].(string); ok {
				if new, delta := truncateString(s, threshold); delta > 0 {
					bm["arguments"] = new
					saved += delta
				}
			}
		}
	}

	// message.details.truncation.content holds pre-compaction snapshots —
	// large and redundant once a summary has been taken.
	if details, ok := msg["details"].(map[string]any); ok {
		if trunc, ok := details["truncation"].(map[string]any); ok {
			if s, ok := trunc["content"].(string); ok {
				if new, delta := truncateString(s, threshold); delta > 0 {
					trunc["content"] = new
					saved += delta
				}
			}
		}
	}
	return saved
}
