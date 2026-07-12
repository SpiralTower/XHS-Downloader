package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type ExtractParams struct {
	URL      string  `json:"url"`
	Download bool    `json:"download"`
	Index    []any   `json:"index"`
	Cookie   *string `json:"cookie"`
	Proxy    *string `json:"proxy"`
	Skip     bool    `json:"skip"`
}

type extractRequest struct {
	URL      *string `json:"url"`
	Download bool    `json:"download"`
	Index    []any   `json:"index"`
	Cookie   *string `json:"cookie"`
	Proxy    *string `json:"proxy"`
	Skip     bool    `json:"skip"`
}

type ExtractResponse struct {
	Message string         `json:"message"`
	Params  ExtractParams  `json:"params"`
	Data    map[string]any `json:"data"`
}

func decodeExtractRequest(reader io.Reader) (ExtractParams, error) {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	var request extractRequest
	if err := decoder.Decode(&request); err != nil {
		return ExtractParams{}, fmt.Errorf("decode request: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return ExtractParams{}, err
	}
	if request.URL == nil {
		return ExtractParams{}, errors.New("field url is required")
	}
	if _, err := selectedIndexes(request.Index); err != nil {
		return ExtractParams{}, fmt.Errorf("field index: %w", err)
	}
	return ExtractParams{
		URL:      *request.URL,
		Download: request.Download,
		Index:    request.Index,
		Cookie:   cleanOptionalString(request.Cookie),
		Proxy:    cleanOptionalString(request.Proxy),
		Skip:     request.Skip,
	}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing content: %w", err)
	}
	return errors.New("request body must contain one JSON object")
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	cleaned := strings.TrimSpace(*value)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func selectedIndexes(values []any) (map[int]struct{}, error) {
	if len(values) == 0 {
		return nil, nil
	}
	selected := make(map[int]struct{}, len(values))
	for _, value := range values {
		raw := stringValue(value)
		var index int
		if _, err := fmt.Sscan(raw, &index); err != nil || index < 1 || !bytes.Equal(bytes.TrimSpace([]byte(raw)), []byte(fmt.Sprint(index))) {
			return nil, fmt.Errorf("invalid image index %q", raw)
		}
		selected[index] = struct{}{}
	}
	return selected, nil
}
