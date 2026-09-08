package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	rolloutBufferSize = 64 * 1024
	maxMetadataBytes  = 64 * 1024
	maxPreviewRunes   = 1024
	maxJSONDepth      = 10000 // Match encoding/json's nesting limit.
)

// Small records use encoding/json directly. For larger records, retain only
// the fields discovery consumes. The projection has a fixed set of fields,
// bounded strings and one bounded message preview, independent of input size.
type jsonProjection struct {
	fields      map[string]*jsonProjection
	preview     bool
	trimLeading bool
	content     bool
	source      bool
}

var rolloutProjection = func() *jsonProjection {
	scalar := &jsonProjection{}
	preview := &jsonProjection{preview: true, trimLeading: true}
	object := func(fields map[string]*jsonProjection) *jsonProjection {
		return &jsonProjection{fields: fields}
	}
	return object(map[string]*jsonProjection{
		"timestamp": scalar,
		"type":      scalar,
		"payload": object(map[string]*jsonProjection{
			"id": scalar, "cwd": scalar, "cli_version": scalar,
			"agent_nickname": {preview: true}, "source": {source: true},
			"model": scalar, "git": object(map[string]*jsonProjection{"branch": scalar}),
			"type": scalar, "name": scalar, "role": scalar,
			"content":            {content: true},
			"last_agent_message": preview,
			"info": object(map[string]*jsonProjection{
				"total_token_usage": object(map[string]*jsonProjection{
					"input_tokens": scalar, "cached_input_tokens": scalar,
					"output_tokens": scalar, "reasoning_output_tokens": scalar,
				}),
			}),
		}),
	})
}()

var contentProjection = &jsonProjection{fields: map[string]*jsonProjection{
	"type": {}, "text": {preview: true},
}}

var firstContentProjection = &jsonProjection{fields: map[string]*jsonProjection{
	"type": {}, "text": {preview: true, trimLeading: true},
}}

// rolloutLineReader exposes exactly one line, including its delimiter. This
// prevents the projection's buffered reader from reading into the next record.
type rolloutLineReader struct {
	reader     *bufio.Reader
	pending    []byte
	done       bool
	terminated bool
	consumed   int64
	readErr    error
}

func (r *rolloutLineReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 && !r.done {
		var err error
		r.pending, err = r.reader.ReadSlice('\n')
		switch err {
		case nil:
			r.done, r.terminated = true, true
		case io.EOF:
			r.done = true
		case bufio.ErrBufferFull:
		default:
			r.done, r.readErr = true, err
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	r.consumed += int64(n)
	if len(r.pending) == 0 && r.done {
		if r.readErr != nil {
			return n, r.readErr
		}
		return n, io.EOF
	}
	return n, nil
}

func readRolloutRecord(reader *bufio.Reader) ([]byte, int64, bool, error) {
	fragment, err := reader.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		if err == io.EOF {
			return fragment, int64(len(fragment)), false, nil
		}
		return fragment, int64(len(fragment)), err == nil, err
	}
	line := &rolloutLineReader{reader: reader, pending: fragment}
	parser := rolloutJSONParser{reader: bufio.NewReaderSize(line, 4096)}
	value, parseErr := parser.value(rolloutProjection, 0, nil)
	if parseErr == nil {
		if _, err := parser.peek(); err != io.EOF {
			parseErr = errInvalidRolloutJSON
		}
	}
	// Even malformed or excessively nested records must be drained through
	// the newline so subsequent activity is readable at the exact next offset.
	_, drainErr := io.Copy(io.Discard, parser.reader)
	if line.readErr != nil {
		return nil, 0, false, line.readErr
	}
	if drainErr != nil {
		return nil, 0, false, drainErr
	}
	if parseErr != nil {
		return nil, line.consumed, line.terminated, nil
	}
	projected, err := json.Marshal(value)
	return projected, line.consumed, line.terminated, err
}

var errInvalidRolloutJSON = errors.New("invalid rollout JSON")

type rolloutJSONParser struct {
	reader *bufio.Reader
}

// peek skips JSON whitespace without consuming the next token.
func (p *rolloutJSONParser) peek() (byte, error) {
	for {
		b, err := p.reader.Peek(1)
		if err != nil {
			return 0, err
		}
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			_, _ = p.reader.ReadByte()
		default:
			return b[0], nil
		}
	}
}

func (p *rolloutJSONParser) take(want byte) error {
	b, err := p.peek()
	if err != nil {
		return err
	}
	if b != want {
		return errInvalidRolloutJSON
	}
	_, err = p.reader.ReadByte()
	return err
}

func (p *rolloutJSONParser) value(rule *jsonProjection, depth int, subagent *bool) (any, error) {
	if rule != nil && rule.source {
		found := false
		_, err := p.value(nil, depth, &found)
		if found {
			return "subagent", err
		}
		return nil, err
	}
	b, err := p.peek()
	if err != nil {
		return nil, err
	}
	if depth >= maxJSONDepth && (b == '{' || b == '[') {
		return nil, errInvalidRolloutJSON
	}
	switch b {
	case '{':
		_, _ = p.reader.ReadByte()
		var result map[string]any
		if rule != nil {
			result = make(map[string]any)
		}
		if b, _ := p.peek(); b == '}' {
			_, err := p.reader.ReadByte()
			return result, err
		}
		for {
			keyLimit := 0
			if rule != nil {
				keyLimit = 256
			}
			key, _, err := p.string(keyLimit, false, false, subagent)
			if err != nil {
				return nil, err
			}
			if err := p.take(':'); err != nil {
				return nil, err
			}
			var child *jsonProjection
			if rule != nil {
				child = rule.fields[key]
				if child == nil {
					// Match encoding/json's case-insensitive struct fields.
					for name, candidate := range rule.fields {
						if strings.EqualFold(key, name) {
							key, child = name, candidate
							break
						}
					}
				}
			}
			v, err := p.value(child, depth+1, subagent)
			if err != nil {
				return nil, err
			}
			if child != nil {
				result[key] = v
			}
			b, err := p.peek()
			if err != nil {
				return nil, err
			}
			if b == '}' {
				_, err := p.reader.ReadByte()
				return result, err
			}
			if err := p.take(','); err != nil {
				return nil, err
			}
		}
	case '[':
		return p.array(rule, depth, subagent)
	case '"':
		limit := 0
		preview := rule != nil && rule.preview
		if rule != nil {
			limit = maxMetadataBytes
		}
		s, overflow, err := p.string(limit, preview, rule != nil && rule.trimLeading, subagent)
		if overflow && !preview {
			// Never manufacture a truncated session ID, cwd or tool name.
			return nil, err
		}
		return s, err
	case 't', 'f', 'n':
		literal, value := "null", any(nil)
		if b == 't' {
			literal, value = "true", true
		} else if b == 'f' {
			literal, value = "false", false
		}
		for i := range literal {
			got, err := p.reader.ReadByte()
			if err != nil {
				return nil, err
			}
			if got != literal[i] {
				return nil, errInvalidRolloutJSON
			}
		}
		return value, nil
	default:
		return p.number(rule != nil)
	}
}

func (p *rolloutJSONParser) array(rule *jsonProjection, depth int, subagent *bool) (any, error) {
	_, _ = p.reader.ReadByte()
	var preview strings.Builder
	runes := 0
	tailContent, invalidContent := false, false
	emit := func(r rune) {
		if runes < maxPreviewRunes {
			preview.WriteRune(r)
			runes++
		} else if !unicode.IsSpace(r) {
			tailContent = true
		}
	}
	if b, _ := p.peek(); b != ']' {
		for {
			var child *jsonProjection
			if rule != nil && rule.content {
				child = contentProjection
				if preview.Len() == 0 {
					child = firstContentProjection
				}
			}
			v, err := p.value(child, depth+1, subagent)
			if err != nil {
				return nil, err
			}
			if block, ok := v.(map[string]any); ok && child != nil {
				text, _ := block["text"].(string)
				kind, _ := block["type"].(string)
				for _, field := range []string{"type", "text"} {
					if value := block[field]; value != nil {
						if _, ok := value.(string); !ok {
							invalidContent = true
						}
					}
				}
				if text != "" && (kind == "input_text" || kind == "output_text") {
					if preview.Len() > 0 {
						emit('\n')
					}
					for _, r := range text {
						emit(r)
					}
				}
			} else if child != nil && v != nil {
				invalidContent = true
			}
			b, err := p.peek()
			if err != nil {
				return nil, err
			}
			if b == ']' {
				break
			}
			if err := p.take(','); err != nil {
				return nil, err
			}
		}
	}
	_, err := p.reader.ReadByte()
	if rule == nil {
		return nil, err
	}
	if invalidContent {
		return []any{false}, err // Preserve responseItemBlock's type error.
	}
	if tailContent {
		// Keep TrimSpace from stripping whitespace inside the preview when
		// the original message has more text after the retained prefix.
		preview.WriteRune('…')
	}
	if rule != nil && rule.content && preview.Len() > 0 {
		return []any{map[string]any{"type": "input_text", "text": preview.String()}}, err
	}
	return []any{}, err
}

// string validates the whole JSON string, including discarded suffixes.
// Retained metadata has a byte cap; previews skip leading whitespace and
// retain a bounded number of decoded runes. Escaped surrogate pairs are
// combined before counting, matching encoding/json's string decoding.
func (p *rolloutJSONParser) string(limit int, preview, trimLeading bool, subagent *bool) (string, bool, error) {
	if err := p.take('"'); err != nil {
		return "", false, err
	}
	if subagent != nil && limit < len("subagent") {
		limit = len("subagent")
	}
	var out strings.Builder
	count, size := 0, 0
	overflow := false
	tailContent := false
	emit := func(r rune) {
		if trimLeading && count == 0 && unicode.IsSpace(r) {
			return
		}
		if (preview && count < maxPreviewRunes) || (!preview && size+utf8.RuneLen(r) <= limit) {
			out.WriteRune(r)
		} else {
			overflow = true
			if !unicode.IsSpace(r) {
				tailContent = true
			}
		}
		if count <= maxPreviewRunes {
			count++
		}
		if size <= limit {
			size += utf8.RuneLen(r)
		}
	}
	var high rune
	for {
		r, _, err := p.reader.ReadRune()
		if err != nil {
			return "", false, err
		}
		if r == '"' {
			if high != 0 {
				emit(utf8.RuneError)
			}
			if preview && tailContent {
				out.WriteRune('…')
			}
			s := out.String()
			if subagent != nil && !overflow && s == "subagent" {
				*subagent = true
			}
			if overflow && !preview {
				return "", true, nil
			}
			return s, overflow, nil
		}
		if r < 0x20 {
			return "", false, errInvalidRolloutJSON
		}
		escapedUnicode := false
		if r == '\\' {
			b, err := p.reader.ReadByte()
			if err != nil {
				return "", false, err
			}
			switch b {
			case '"', '\\', '/':
				r = rune(b)
			case 'b':
				r = '\b'
			case 'f':
				r = '\f'
			case 'n':
				r = '\n'
			case 'r':
				r = '\r'
			case 't':
				r = '\t'
			case 'u':
				r, escapedUnicode = 0, true
				for range 4 {
					b, err := p.reader.ReadByte()
					if err != nil {
						return "", false, err
					}
					var digit byte
					switch {
					case b >= '0' && b <= '9':
						digit = b - '0'
					case b >= 'a' && b <= 'f':
						digit = b - 'a' + 10
					case b >= 'A' && b <= 'F':
						digit = b - 'A' + 10
					default:
						return "", false, errInvalidRolloutJSON
					}
					r = r*16 + rune(digit)
				}
			default:
				return "", false, errInvalidRolloutJSON
			}
		}
		if high != 0 {
			if escapedUnicode && r >= 0xdc00 && r <= 0xdfff {
				emit(utf16.DecodeRune(high, r))
				high = 0
				continue
			}
			emit(utf8.RuneError)
			high = 0
		}
		if escapedUnicode && r >= 0xd800 && r <= 0xdbff {
			high = r
		} else if escapedUnicode && r >= 0xdc00 && r <= 0xdfff {
			emit(utf8.RuneError)
		} else {
			emit(r)
		}
	}
}

func (p *rolloutJSONParser) number(keep bool) (any, error) {
	var token [64]byte
	n := 0
	take := func() {
		b, _ := p.reader.ReadByte()
		if n < len(token) {
			token[n] = b
		}
		if n <= len(token) {
			n++
		}
	}
	peek := func() byte {
		b, err := p.reader.Peek(1)
		if err != nil {
			return 0
		}
		return b[0]
	}
	digits := func() bool {
		any := false
		for b := peek(); b >= '0' && b <= '9'; b = peek() {
			take()
			any = true
		}
		return any
	}
	if peek() == '-' {
		take()
	}
	if peek() == '0' {
		take()
	} else if !digits() {
		return nil, errInvalidRolloutJSON
	}
	if peek() == '.' {
		take()
		if !digits() {
			return nil, errInvalidRolloutJSON
		}
	}
	if b := peek(); b == 'e' || b == 'E' {
		take()
		if b := peek(); b == '+' || b == '-' {
			take()
		}
		if !digits() {
			return nil, errInvalidRolloutJSON
		}
	}
	if !keep {
		return nil, nil
	}
	if n > len(token) {
		// All selected numeric fields are ints. Preserve their decode failure
		// for numbers too large to retain, without buffering arbitrary digits.
		return json.Number("1e999999999"), nil
	}
	return json.Number(token[:n]), nil
}
