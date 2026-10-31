package main

import (
	"encoding/base64"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

// checkResponse must return only the results of the first setID group
// and skip items without a setID
func TestCheckResponseGrouping(t *testing.T) {
	body := `{"data":[
		{"0":"id-no-set","10":"a.rar","11":"rar","19":""},
		{"0":"id-1","10":"a.rar","11":"rar","19":"setA","sig":"s1"},
		{"0":"id-2","10":"b.rar","11":"rar","19":"setA","sig":"s2"},
		{"0":"id-3","10":"c.rar","11":"rar","19":"setB","sig":"s3"}
	]}`
	results, err := checkResponse([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results from first setID group, got %d", len(results))
	}
	if results[0].ID != "id-1" || results[1].ID != "id-2" {
		t.Errorf("unexpected result IDs: %v, %v", results[0].ID, results[1].ID)
	}
}

func TestCheckResponseErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"invalid json", `not json`},
		{"empty data", `{"data":[]}`},
		{"missing data", `{}`},
		{"all items without setID", `{"data":[{"0":"a","19":""}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := checkResponse([]byte(tc.body)); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// encodeFileName produces unpadded base64 name:ext
func TestEncodeFileName(t *testing.T) {
	got := encodeFileName("file name.rar", "rar")
	wantName := strings.TrimRight(base64.StdEncoding.EncodeToString([]byte("file name.rar")), "=")
	wantExt := strings.TrimRight(base64.StdEncoding.EncodeToString([]byte("rar")), "=")
	if got != wantName+":"+wantExt {
		t.Errorf("encodeFileName = %q, want %q", got, wantName+":"+wantExt)
	}
}

// the download form must contain autoNZB and indexed sig fields
func TestMakeDownloadFormData(t *testing.T) {
	results := []easynewsResult{
		{ID: "1", FileName: "a.rar", Extension: "rar", Sig: "sigA"},
		{ID: "2", FileName: "b.rar", Extension: "rar", Sig: "sigB"},
	}
	body, contentType, err := makeDownloadFormData(results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q, err=%v", contentType, err)
	}
	reader := multipart.NewReader(body, params["boundary"])
	form, err := reader.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("reading form: %v", err)
	}
	if got := form.Value["autoNZB"]; len(got) != 1 || got[0] != "1" {
		t.Errorf("autoNZB = %v, want [1]", got)
	}
	if got := form.Value["0&sig=sigA"]; len(got) != 1 {
		t.Errorf("expected field '0&sig=sigA', got keys %v", keysOf(form.Value))
	}
	if got := form.Value["1&sig=sigB"]; len(got) != 1 {
		t.Errorf("expected field '1&sig=sigB', got keys %v", keysOf(form.Value))
	}
}

func keysOf(v map[string][]string) []string {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	return keys
}
