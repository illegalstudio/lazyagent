package compact

// compactCodexLine rewrites a single Codex CLI JSONL envelope. Every
// transcript line wraps a payload under `.payload`; the payload shape
// depends on `.type`.
//
// The big fields in practice:
//   - response_item / function_call_output → payload.output, payload.content
//   - response_item / message              → payload.content[].text
//   - event_msg / agent_message etc.       → payload.message (when long)
//
// Structure of the envelope is preserved so `codex resume` keeps working.
func compactCodexLine(entry map[string]any, threshold int64) int64 {
	var saved int64

	payload, ok := entry["payload"].(map[string]any)
	if !ok {
		return 0
	}

	// function_call_output: the big offender. Output can be multi-MB.
	if s, ok := payload["output"].(string); ok {
		if new, delta := truncateString(s, threshold); delta > 0 {
			payload["output"] = new
			saved += delta
		}
	}
	// Some variants nest the output under `result`.
	if s, ok := payload["result"].(string); ok {
		if new, delta := truncateString(s, threshold); delta > 0 {
			payload["result"] = new
			saved += delta
		}
	}

	// response_item blocks with `content: [{type, text}, ...]`.
	if arr, ok := payload["content"].([]any); ok {
		for _, block := range arr {
			bm, ok := block.(map[string]any)
			if !ok {
				continue
			}
			for _, key := range []string{"text", "input_text", "output_text"} {
				if s, ok := bm[key].(string); ok {
					if new, delta := truncateString(s, threshold); delta > 0 {
						bm[key] = new
						saved += delta
					}
				}
			}
		}
	}

	// Long agent_message payloads embed the text in .message.
	if s, ok := payload["message"].(string); ok {
		if new, delta := truncateString(s, threshold); delta > 0 {
			payload["message"] = new
			saved += delta
		}
	}
	// Function call arguments (usually a JSON blob as string) can also bloat.
	if s, ok := payload["arguments"].(string); ok {
		if new, delta := truncateString(s, threshold); delta > 0 {
			payload["arguments"] = new
			saved += delta
		}
	}

	return saved
}
