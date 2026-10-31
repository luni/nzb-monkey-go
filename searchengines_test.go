package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testNzb = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<file poster="tester" date="1234567890" subject="test [1/3] file (1/5)">
<groups><group>alt.binaries.test</group></groups>
<segments><segment bytes="100" number="1">seg1@news</segment></segments>
</file>
</nzb>`

func testEngine(searchBody string) (SearchEngine, *httptest.Server) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			fmt.Fprint(w, searchBody)
			return
		}
		fmt.Fprint(w, testNzb)
	}))
	return SearchEngine{
		name:        "TestEngine",
		searchURL:   srv.URL + "/search?q=%s",
		downloadURL: srv.URL + "/dl/%s.nzb",
		jsonPath:    "data.content.0.id",
		search:      jsonSearch,
	}, srv
}

// jsonSearch must return an error, not panic, on unexpected JSON shapes
// (regression test for unchecked type assertions)
func TestJsonSearchMalformedResponses(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test"}}

	cases := []struct {
		name string
		body string
	}{
		{"error string instead of object", `{"data":"error"}`},
		{"content not an array", `{"data":{"content":"oops"}}`},
		{"array index out of range", `{"data":{"content":[]}}`},
		{"id not a scalar", `{"data":{"content":[{"id":{"x":1}}]}}`},
		{"empty object", `{}`},
		{"array as root", `["x"]`},
		{"null mid path", `{"data":{"content":[null]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, srv := testEngine(tc.body)
			defer srv.Close()

			if err := engine.search(engine, engine.name); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// jsonSearch must return an error on invalid JSON
func TestJsonSearchInvalidJSON(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test"}}
	engine, srv := testEngine("this is not json")
	defer srv.Close()

	if err := engine.search(engine, engine.name); err == nil {
		t.Error("expected error, got nil")
	}
}

// happy path: valid JSON response leads to NZB download and processing
func TestJsonSearchSuccess(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test", Title: "test"}}
	results = nil
	// all thresholds allow an incomplete NZB, so it gets appended rather than
	// processed (processFoundNzb would terminate via os.Exit)
	conf.Nzbcheck = NZBcheck{BestNZB: true, MaxMissingFiles: 99, MaxMissingSegmentsPercent: 100}

	downloaded := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			fmt.Fprint(w, `{"data":{"content":[{"id":12345}]}}`)
		case "/dl/12345.nzb":
			downloaded = true
			fmt.Fprint(w, testNzb)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	engine := SearchEngine{
		name:        "TestEngine",
		searchURL:   srv.URL + "/search?q=%s",
		downloadURL: srv.URL + "/dl/%s.nzb",
		jsonPath:    "data.content.0.id",
		search:      jsonSearch,
	}

	if err := engine.search(engine, engine.name); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !downloaded {
		t.Error("download URL was never requested")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result appended, got %d", len(results))
	}
	if results[0].SearchEngine != "TestEngine" {
		t.Errorf("SearchEngine = %q, want %q", results[0].SearchEngine, "TestEngine")
	}
}
