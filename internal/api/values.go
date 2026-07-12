package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func asMap(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func asSlice(value any) []any {
	result, _ := value.([]any)
	return result
}

func mapAt(root map[string]any, keys ...string) (map[string]any, bool) {
	var current any = root
	for _, key := range keys {
		object, ok := asMap(current)
		if !ok {
			return nil, false
		}
		current, ok = object[key]
		if !ok {
			return nil, false
		}
	}
	return asMap(current)
}

func valueAt(root any, keys ...string) any {
	current := root
	for _, key := range keys {
		object, ok := asMap(current)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func floatValue(value any) float64 {
	switch typed := value.(type) {
	case json.Number:
		result, _ := typed.Float64()
		return result
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		result, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return result
	default:
		return 0
	}
}

func firstString(values ...any) string {
	for _, value := range values {
		if result := strings.TrimSpace(stringValue(value)); result != "" {
			return result
		}
	}
	return ""
}

func interfaceOr(value, fallback any) any {
	if value == nil || stringValue(value) == "" {
		return fallback
	}
	return value
}
