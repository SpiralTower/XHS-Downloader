package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

var ErrNoteDataNotFound = errors.New("note data not found in initial state")

func parseInitialState(html, expectedID string) (map[string]any, error) {
	payloads := initialStatePayloads(html)
	if len(payloads) == 0 {
		return nil, ErrNoteDataNotFound
	}

	var decodeErrors []error
	for _, payload := range payloads {
		payload = normalizeJavaScriptJSON(payload)
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()

		var root map[string]any
		if err := decoder.Decode(&root); err != nil {
			decodeErrors = append(decodeErrors, err)
			continue
		}
		if note, ok := noteFromInitialState(root, expectedID); ok {
			return note, nil
		}
	}
	if len(decodeErrors) == len(payloads) {
		return nil, fmt.Errorf("decode initial state: %w", errors.Join(decodeErrors...))
	}
	return nil, ErrNoteDataNotFound
}

func noteFromInitialState(root map[string]any, expectedID string) (map[string]any, bool) {
	if note, ok := mapAt(root, "noteData", "data", "noteData"); ok {
		if expectedID == "" || stringValue(note["noteId"]) == expectedID {
			return note, true
		}
	}

	detailMap, ok := mapAt(root, "note", "noteDetailMap")
	if !ok || len(detailMap) == 0 {
		return nil, false
	}
	keys := make([]string, 0, len(detailMap))
	for key := range detailMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		entry, ok := asMap(detailMap[key])
		if !ok {
			continue
		}
		note, ok := asMap(entry["note"])
		if !ok {
			continue
		}
		if expectedID == "" || stringValue(note["noteId"]) == expectedID {
			return note, true
		}
	}
	return nil, false
}

func initialStatePayload(html string) ([]byte, error) {
	payloads := initialStatePayloads(html)
	if len(payloads) == 0 {
		return nil, ErrNoteDataNotFound
	}
	return payloads[0], nil
}

func initialStatePayloads(html string) [][]byte {
	const marker = "window.__INITIAL_STATE__"
	payloads := make([][]byte, 0, 1)
	searchEnd := len(html)
	for searchEnd > 0 {
		index := strings.LastIndex(html[:searchEnd], marker)
		if index < 0 {
			break
		}
		searchEnd = index
		if index > 0 && isIdentifierByte(html[index-1]) {
			continue
		}
		if !isInitialStateScriptStart(html, index) {
			continue
		}

		start := index + len(marker)
		for start < len(html) && unicode.IsSpace(rune(html[start])) {
			start++
		}
		if start >= len(html) || html[start] != '=' {
			continue
		}
		start++
		for start < len(html) && unicode.IsSpace(rune(html[start])) {
			start++
		}
		if start >= len(html) || html[start] != '{' {
			continue
		}
		if payload, ok := balancedJSONObject(html, start); ok {
			payloads = append(payloads, []byte(payload))
		}
	}
	return payloads
}

func isInitialStateScriptStart(html string, markerIndex int) bool {
	prefix := html[:markerIndex]
	lowerPrefix := strings.ToLower(prefix)
	scriptStart := strings.LastIndex(lowerPrefix, "<script")
	if scriptStart < 0 {
		return strings.TrimSpace(prefix) == ""
	}
	if scriptEnd := strings.LastIndex(lowerPrefix, "</script"); scriptEnd > scriptStart {
		return false
	}
	tagEnd := strings.Index(prefix[scriptStart:], ">")
	if tagEnd < 0 {
		return false
	}
	return strings.TrimSpace(prefix[scriptStart+tagEnd+1:]) == ""
}

func balancedJSONObject(value string, start int) (string, bool) {
	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(value); index++ {
		character := value[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				inString = false
			}
			continue
		}
		switch character {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return value[start : index+1], true
			}
		}
	}
	return "", false
}

func normalizeJavaScriptJSON(payload []byte) []byte {
	var output bytes.Buffer
	inString := false
	escaped := false
	for index := 0; index < len(payload); {
		character := payload[index]
		if inString {
			output.WriteByte(character)
			index++
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
			continue
		}
		if character == '"' {
			inString = true
			output.WriteByte(character)
			index++
			continue
		}

		replaced := false
		for _, token := range []string{"undefined", "NaN", "-Infinity", "Infinity"} {
			if bytes.HasPrefix(payload[index:], []byte(token)) && tokenBoundary(payload, index, len(token)) {
				output.WriteString("null")
				index += len(token)
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		output.WriteByte(character)
		index++
	}
	return output.Bytes()
}

func tokenBoundary(payload []byte, start, length int) bool {
	beforeOK := start == 0 || !isIdentifierByte(payload[start-1])
	after := start + length
	afterOK := after == len(payload) || !isIdentifierByte(payload[after])
	return beforeOK && afterOK
}

func isIdentifierByte(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}
