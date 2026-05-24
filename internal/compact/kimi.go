package compact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Kimi sessions are directories. The bulky, resume-relevant files are the
// main wire/context streams plus the same files inside subagent directories.
var kimiBulkJSONL = []string{"wire.jsonl", "context.jsonl"}

// compactKimiLine rewrites one Kimi JSONL entry, preserving event/tool-call
// structure while truncating large content, thinking, arguments, and tool
// result payloads.
func compactKimiLine(entry map[string]any, threshold int64) int64 {
	var saved int64
	if msg, ok := entry["message"].(map[string]any); ok {
		saved += compactKimiWireMessage(msg, threshold)
	}
	saved += compactKimiContextEntry(entry, threshold)
	return saved
}

func compactKimiWireMessage(msg map[string]any, threshold int64) int64 {
	msgType, _ := msg["type"].(string)
	payload, ok := msg["payload"].(map[string]any)
	if !ok {
		return 0
	}
	return compactKimiPayload(msgType, payload, threshold)
}

func compactKimiPayload(msgType string, payload map[string]any, threshold int64) int64 {
	var saved int64
	switch msgType {
	case "TurnBegin":
		saved += compactKimiValueField(payload, "user_input", threshold)
	case "ContentPart":
		saved += compactKimiTextFields(payload, threshold)
	case "ToolCall":
		saved += compactKimiFunction(payload, threshold)
		saved += truncateKimiStringField(payload, "arguments", threshold)
	case "ToolCallPart":
		saved += truncateKimiStringField(payload, "arguments_part", threshold)
	case "ToolResult":
		saved += truncateKimiDeepField(payload, "return_value", threshold)
		saved += truncateKimiDeepField(payload, "result", threshold)
		saved += truncateKimiDeepField(payload, "content", threshold)
		saved += truncateKimiDeepField(payload, "output", threshold)
	case "SubagentEvent":
		if event, ok := payload["event"].(map[string]any); ok {
			saved += compactKimiNestedEvent(event, threshold)
		}
	}

	// Be tolerant of small schema drift in Kimi wire payloads without touching
	// IDs or timestamps.
	saved += compactKimiFunction(payload, threshold)
	saved += compactKimiValueField(payload, "content", threshold)
	saved += compactKimiValueField(payload, "user_input", threshold)
	saved += compactKimiTextFields(payload, threshold)
	return saved
}

func compactKimiNestedEvent(event map[string]any, threshold int64) int64 {
	msgType, _ := event["type"].(string)
	payload, ok := event["payload"].(map[string]any)
	if !ok {
		return 0
	}
	return compactKimiPayload(msgType, payload, threshold)
}

func compactKimiContextEntry(entry map[string]any, threshold int64) int64 {
	var saved int64
	saved += compactKimiValueField(entry, "content", threshold)
	saved += compactKimiToolCalls(entry, threshold)
	saved += compactKimiFunction(entry, threshold)
	return saved
}

func compactKimiValueField(m map[string]any, key string, threshold int64) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	return compactKimiValue(v, func(new any) { m[key] = new }, threshold)
}

func compactKimiValue(v any, set func(any), threshold int64) int64 {
	switch t := v.(type) {
	case string:
		if newVal, delta := truncateString(t, threshold); delta > 0 {
			set(newVal)
			return delta
		}
	case []any:
		return compactKimiArray(t, threshold)
	case map[string]any:
		return compactKimiObject(t, threshold)
	}
	return 0
}

func compactKimiArray(arr []any, threshold int64) int64 {
	var saved int64
	for i, item := range arr {
		idx := i
		saved += compactKimiValue(item, func(new any) { arr[idx] = new }, threshold)
	}
	return saved
}

func compactKimiObject(m map[string]any, threshold int64) int64 {
	var saved int64
	saved += compactKimiTextFields(m, threshold)
	saved += compactKimiFunction(m, threshold)
	saved += compactKimiToolCalls(m, threshold)
	for _, key := range []string{"content", "children"} {
		v, ok := m[key]
		if !ok {
			continue
		}
		switch v.(type) {
		case []any, map[string]any:
			saved += compactKimiValue(v, func(new any) { m[key] = new }, threshold)
		}
	}
	if src, ok := m["source"].(map[string]any); ok {
		saved += truncateKimiStringField(src, "data", threshold)
	}
	return saved
}

func compactKimiTextFields(m map[string]any, threshold int64) int64 {
	var saved int64
	for _, key := range []string{"text", "content", "output", "input_text", "output_text"} {
		saved += truncateKimiStringField(m, key, threshold)
	}
	for _, key := range []string{"think", "thinking"} {
		saved += truncateKimiStringField(m, key, threshold*2)
	}
	return saved
}

func compactKimiFunction(m map[string]any, threshold int64) int64 {
	fn, ok := m["function"].(map[string]any)
	if !ok {
		return 0
	}
	return truncateKimiStringField(fn, "arguments", threshold)
}

func compactKimiToolCalls(m map[string]any, threshold int64) int64 {
	arr, ok := m["tool_calls"].([]any)
	if !ok {
		return 0
	}
	var saved int64
	for _, item := range arr {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		saved += compactKimiFunction(call, threshold)
		saved += truncateKimiStringField(call, "arguments", threshold)
	}
	return saved
}

func truncateKimiStringField(m map[string]any, key string, threshold int64) int64 {
	s, ok := m[key].(string)
	if !ok {
		return 0
	}
	if newVal, delta := truncateString(s, threshold); delta > 0 {
		m[key] = newVal
		return delta
	}
	return 0
}

func truncateKimiDeepField(m map[string]any, key string, threshold int64) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case string:
		if newVal, delta := truncateString(t, threshold); delta > 0 {
			m[key] = newVal
			return delta
		}
	case map[string]any, []any:
		return truncateKimiDeep(t, threshold)
	}
	return 0
}

func truncateKimiDeep(v any, threshold int64) int64 {
	var saved int64
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if s, ok := child.(string); ok {
				if newVal, delta := truncateString(s, threshold); delta > 0 {
					t[k] = newVal
					saved += delta
				}
				continue
			}
			saved += truncateKimiDeep(child, threshold)
		}
	case []any:
		for i, child := range t {
			if s, ok := child.(string); ok {
				if newVal, delta := truncateString(s, threshold); delta > 0 {
					t[i] = newVal
					saved += delta
				}
				continue
			}
			saved += truncateKimiDeep(child, threshold)
		}
	}
	return saved
}

func kimiJSONLFiles(dir string) []string {
	var files []string
	for _, name := range kimiBulkJSONL {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files = append(files, path)
		}
	}
	for _, name := range kimiBulkJSONL {
		matches, _ := filepath.Glob(filepath.Join(dir, "subagents", "*", name))
		files = append(files, matches...)
	}
	return files
}

func kimiRawOutputs(dir string) []string {
	matches, _ := filepath.Glob(filepath.Join(dir, "subagents", "*", "output"))
	return matches
}

// kimiLiveDirBytes returns the total size of a Kimi session directory,
// excluding backup/temp sidecars generated by compact.
func kimiLiveDirBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if filepath.Ext(name) == ".bak" || strings.HasSuffix(name, ".compact.tmp") {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func compactKimiSession(dir string, threshold int64, backup bool) (int64, error) {
	if err := guardPath(dir); err != nil {
		return 0, err
	}
	for _, path := range kimiJSONLFiles(dir) {
		if _, err := rewriteFile(path, compactKimiLine, threshold, backup); err != nil {
			return 0, fmt.Errorf("%s: %w", relKimiPath(dir, path), err)
		}
	}
	for _, path := range kimiRawOutputs(dir) {
		if err := compactKimiRawOutput(path, threshold, backup); err != nil {
			return 0, fmt.Errorf("%s: %w", relKimiPath(dir, path), err)
		}
	}
	return kimiLiveDirBytes(dir), nil
}

func compactKimiRawOutput(path string, threshold int64, backup bool) error {
	if err := guardPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= threshold {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	newVal, delta := truncateString(string(data), threshold)
	if delta <= 0 {
		return nil
	}
	if backup {
		if err := copyFile(path, path+".bak"); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return os.WriteFile(path, []byte(newVal), info.Mode())
}

// estimateKimiSession simulates Kimi directory compaction without writing.
func estimateKimiSession(dir string, threshold int64) (int64, error) {
	total := kimiLiveDirBytes(dir)
	for _, path := range kimiJSONLFiles(dir) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		after, err := estimateJSONL(path, compactKimiLine, threshold)
		if err != nil {
			continue
		}
		total -= info.Size() - after
	}
	for _, path := range kimiRawOutputs(dir) {
		info, err := os.Stat(path)
		if err != nil || info.Size() <= threshold {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if newVal, delta := truncateString(string(data), threshold); delta > 0 {
			total -= info.Size() - int64(len(newVal))
		}
	}
	return total, nil
}

func relKimiPath(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}
