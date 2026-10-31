package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	conf.General.Targets = nil

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

// htmlSearch must extract the id via regex, build the download URL and
// process the NZB
func TestHtmlSearchSuccess(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test", Title: "test"}}
	results = nil
	conf.Nzbcheck = NZBcheck{BestNZB: true, MaxMissingFiles: 99, MaxMissingSegmentsPercent: 100}
	conf.General.Targets = nil

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		switch r.URL.Path {
		case "/search":
			fmt.Fprint(w, `<a href="/nzb:abc123/">link</a>`)
		case "/dl/abc123.nzb":
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
		regexString: `href="/nzb:(.+?)/"`,
		groupNo:     1,
		search:      htmlSearch,
	}

	if err := engine.search(engine, engine.name); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/dl/abc123.nzb" {
		t.Errorf("download path = %q, want /dl/abc123.nzb", gotPath)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result appended, got %d", len(results))
	}
}

// htmlSearch error paths must return errors, not panic
func TestHtmlSearchErrors(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test"}}

	cases := []struct {
		name        string
		regexString string
		groupNo     int
		searchBody  string
	}{
		{"no regex match", `href="/nzb:(.+?)/"`, 1, `<html>no results</html>`},
		{"invalid regex", `([unclosed`, 1, `href="/nzb:abc/"`},
		{"group number out of range", `href="/nzb:(.+?)/"`, 5, `href="/nzb:abc/"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, srv := testEngine(tc.searchBody)
			defer srv.Close()
			engine.search = htmlSearch
			engine.regexString = tc.regexString
			engine.groupNo = tc.groupNo
			engine.jsonPath = ""

			if err := engine.search(engine, engine.name); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// cleanSearchString applies the engine's stringRegx replacements and
// silently skips broken patterns
func TestCleanSearchString(t *testing.T) {
	nzbking := searchEngines["nzbking"]
	if got := nzbking.cleanSearchString("some.release.name_x264"); got != "some release name x264" {
		t.Errorf("cleanSearchString = %q, want %q", got, "some release name x264")
	}

	broken := SearchEngine{stringRegx: []RegexPattern{
		{pattern: `([unclosed`, replacement: "X"},
		{pattern: `\.`, replacement: " "},
	}}
	if got := broken.cleanSearchString("a.b"); got != "a b" {
		t.Errorf("cleanSearchString = %q, want %q", got, "a b")
	}
}

// newznabSearch must parse the RSS feed, follow the first item link and
// process the NZB
func TestNewznabSearchSuccess(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test", Title: "test"}}
	results = nil
	conf.Nzbcheck = NZBcheck{BestNZB: true, MaxMissingFiles: 99, MaxMissingSegmentsPercent: 100}
	conf.General.Targets = nil

	var gotSearch, gotDownload bool
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			gotSearch = true
			if r.URL.Query().Get("t") != "search" || r.URL.Query().Get("apikey") != "KEY123" {
				t.Errorf("unexpected api query: %s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel>
				<item><link>%s/get?id=1</link></item>
				<item><link>%s/get?id=2</link></item>
			</channel></rss>`, srv.URL, srv.URL)
		case "/get":
			gotDownload = true
			fmt.Fprint(w, testNzb)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	engine := SearchEngine{
		name:      "TestNewznab",
		search:    newznabSearch,
		apikey:    "KEY123",
		searchURL: srv.URL + "/api?t=search&q=%s&apikey=KEY123",
	}

	if err := engine.search(engine, engine.name); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !gotSearch || !gotDownload {
		t.Error("expected both search and download requests")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result appended, got %d", len(results))
	}
}

// newznab error element must surface the API error description
func TestNewznabSearchApiError(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><error code="100" description="Invalid API Key"/>`)
	}))
	defer srv.Close()

	engine := SearchEngine{name: "t", search: newznabSearch, apikey: "BAD", searchURL: srv.URL + "/api?q=%s"}

	err := engine.search(engine, engine.name)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Invalid API Key") {
		t.Errorf("error = %q, want it to contain the API error description", err.Error())
	}
}

// newznab malformed responses and empty result feeds must return errors
func TestNewznabSearchErrors(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test"}}

	cases := []struct {
		name string
		body string
	}{
		{"not xml at all", `garbage`},
		{"empty rss", `<?xml version="1.0"?><rss><channel></channel></rss>`},
		{"item without link", `<?xml version="1.0"?><rss><channel><item><title>x</title></item></channel></rss>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			engine := SearchEngine{name: "t", search: newznabSearch, apikey: "k", searchURL: srv.URL + "/api?q=%s"}
			if err := engine.search(engine, engine.name); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// a failed download URL containing the apikey must be redacted from the
// returned error (the key must never appear in logs)
func TestNewznabSearchKeyRedaction(t *testing.T) {
	args = struct{ Args }{Args: Args{Header: "test"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel>
			<item><link>http://127.0.0.1:1/get?apikey=SECRETKEY</link></item>
		</channel></rss>`)
	}))
	defer srv.Close()

	engine := SearchEngine{name: "t", search: newznabSearch, apikey: "SECRETKEY", searchURL: srv.URL + "/api?q=%s"}
	err := engine.search(engine, engine.name)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "SECRETKEY") {
		t.Errorf("apikey leaked into error message: %s", err.Error())
	}
}
