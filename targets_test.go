package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"mime/multipart"
	"testing"
)

func readMultipartPart(t *testing.T, body *bytes.Buffer, contentType string) []byte {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("invalid content type %q: %v", contentType, err)
	}
	part, err := multipart.NewReader(body, params["boundary"]).NextPart()
	if err != nil {
		t.Fatalf("reading part: %v", err)
	}
	defer part.Close()
	data, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("reading part body: %v", err)
	}
	return data
}

// without compression the multipart part contains the raw NZB
func TestCreateMultipartBodyPlain(t *testing.T) {
	body, contentType, err := createMultipartBody("<nzb>data</nzb>", "test.nzb", "none", []string{"zip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readMultipartPart(t, body, contentType); string(got) != "<nzb>data</nzb>" {
		t.Errorf("part = %q, want raw nzb", got)
	}
}

// "zip" compression must produce gzip-compressed content
func TestCreateMultipartBodyZip(t *testing.T) {
	nzb := "<nzb>data data data data data data data data</nzb>"
	body, contentType, err := createMultipartBody(nzb, "test.nzb", "zip", []string{"zip"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data := readMultipartPart(t, body, contentType)
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("part is not gzip data: %v", err)
	}
	got, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(got) != nzb {
		t.Errorf("unzipped = %q, want %q", got, nzb)
	}
}

// compression not in the target's supported list must stay uncompressed
func TestCreateMultipartBodyUnsupported(t *testing.T) {
	nzb := "<nzb>data</nzb>"
	body, contentType, err := createMultipartBody(nzb, "test.nzb", "zip", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readMultipartPart(t, body, contentType); string(got) != nzb {
		t.Errorf("part = %q, want raw nzb", got)
	}
}

func TestPrettyByteSize(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0.00 B"},
		{512, "512.00 B"},
		{2048, "2.05 KB"},
		{5 * 1000 * 1000, "5.00 MB"},
	}
	for _, tc := range cases {
		if got := prettyByteSize(tc.in); got != tc.want {
			t.Errorf("prettyByteSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
