package api

import (
	"strings"
	"testing"
)

func TestDecodeExtractRequestValidatesPositiveIndexes(t *testing.T) {
	invalid := []string{
		`{"url":"https://www.xiaohongshu.com/explore/note","index":[0]}`,
		`{"url":"https://www.xiaohongshu.com/explore/note","index":[-1]}`,
		`{"url":"https://www.xiaohongshu.com/explore/note","index":[1.5]}`,
		`{"url":"https://www.xiaohongshu.com/explore/note","index":["2x"]}`,
		`{"url":"https://www.xiaohongshu.com/explore/note","index":[true]}`,
	}
	for _, body := range invalid {
		if _, err := decodeExtractRequest(strings.NewReader(body)); err == nil {
			t.Errorf("decodeExtractRequest(%s) accepted invalid index", body)
		}
	}

	params, err := decodeExtractRequest(strings.NewReader(
		`{"url":"https://www.xiaohongshu.com/explore/note","index":[1,"2"]}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Index) != 2 {
		t.Fatalf("Index = %#v", params.Index)
	}
}
