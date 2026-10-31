package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var (
	integrationBinary     string
	integrationBuildOnce  sync.Once
	integrationBuildError error
)

// testBinary builds the real CLI binary once per test run so the integration
// tests exercise the complete program end to end.
func testBinary(t *testing.T) string {
	t.Helper()
	integrationBuildOnce.Do(func() {
		bin := filepath.Join(os.TempDir(), "nzb-monkey-go-integration")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		// the test binary's working directory is the build cache - resolve
		// the package directory from this file's location instead
		_, srcFile, _, _ := runtime.Caller(0)
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = filepath.Dir(srcFile)
		out, err := cmd.CombinedOutput()
		if err != nil {
			integrationBuildError = fmt.Errorf("%v: %s", err, out)
		}
		integrationBinary = bin
	})
	if integrationBuildError != nil {
		t.Fatalf("failed to build test binary: %v", integrationBuildError)
	}
	return integrationBinary
}

// runIntegration executes the built binary with the given config and NZBLNK and
// returns the combined output and the exit code.
func runIntegration(t *testing.T, cfgPath string, nzblnk string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBinary(t), "--config", cfgPath, nzblnk)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run binary: %v\n%s", err, out)
		}
	}
	return string(out), code
}

const integrationConfigTemplate = `[GENERAL]
target = "EXECUTE"
success_wait_time = 1
error_wait_time = 1
debug = false

[SEARCHENGINES]
%s

[EXECUTE]
dontexecute = true
nzbsavepath = "%s"

%s
`

func writeIntegrationConfig(t *testing.T, engines string, savePath string, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.conf")
	content := fmt.Sprintf(integrationConfigTemplate, engines, savePath, extra)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertSavedNzb verifies that an NZB file was written to the save directory
// and returns its content.
func assertSavedNzb(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("save dir not readable: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".nzb") {
			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			return string(content)
		}
	}
	t.Fatalf("no .nzb file written to %s", dir)
	return ""
}

// TestIntegrationNewznabToExecute runs the full pipeline: NZBLNK -> generic
// Newznab engine -> NZB download -> EXECUTE target writes the file.
func TestIntegrationNewznabToExecute(t *testing.T) {

	completeNzb := `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<file poster="tester" date="1737806400" subject="integ-needle [1/1] - &quot;integ-needle.part01.rar&quot; yEnc (1/1)">
<groups><group>alt.binaries.test</group></groups>
<segments><segment bytes="100" number="1">seg1@news</segment></segments>
</file>
</nzb>`

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api":
			fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0"><channel><item><title>hit</title><link>%s/dl/1</link></item></channel></rss>`, srv.URL)
		case r.URL.Path == "/dl/1":
			fmt.Fprint(w, completeNzb)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	saveDir := t.TempDir()
	extra := fmt.Sprintf("[NEWZNAB]\nnewznab1 = \"%s,apikey\"\n", srv.URL)
	cfgPath := writeIntegrationConfig(t, "newznab1 = 1", saveDir, extra)

	out, code := runIntegration(t, cfgPath, "nzblnk:?t=Integration.Title&h=integ-needle&d=1737806400")
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "integ-needle") {
		t.Fatalf("expected the engine to hit the needle, output:\n%s", out)
	}
	nzb := assertSavedNzb(t, saveDir)
	if !strings.Contains(nzb, "integ-needle.part01.rar") {
		t.Fatalf("saved NZB has unexpected content:\n%s", nzb)
	}
}

// TestIntegrationDirectsearchToExecute runs the full pipeline over a fake NNTP
// server: NZBLNK -> boundaries scan -> header scan -> NZB -> EXECUTE.
func TestIntegrationDirectsearchToExecute(t *testing.T) {

	server := newFakeNNTPServer(t, func(first, last int) []string {
		var lines []string
		for i := first; i <= last; i++ {
			subject := fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i)
			if i == 50 || i == 150 {
				subject = fmt.Sprintf("integ-needle [01/01] - \"integ-needle-%d.part01.rar\" yEnc (1/1)", i)
			}
			lines = append(lines, overviewLine(i, subject, "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
		}
		return lines
	})
	// parse the date the fake server emits so the nzblnk date matches
	probe := overviewLine(1, "x", "y", "z", 1)
	parsed, err := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", strings.Split(probe, "\t")[3])
	if err != nil {
		t.Fatalf("fake server date not parseable: %v", err)
	}
	unixDate := parsed.Unix()

	saveDir := t.TempDir()
	extra := fmt.Sprintf(`[DIRECTSEARCH]
host = "127.0.0.1"
port = %d
ssl = false
skip_check = true
username = "u"
password = "p"
connections = 4
hours = 24
forward_hours = 24
step = 50
overview_timeout = 10
overview_retries = 1
use_ansi_codes = false
show_counter = false
one_instance_only = false
boundaries_scanner_step = 20
boundaries_scanner_tolerance = 60
`, server.port())
	cfgPath := writeIntegrationConfig(t, "directsearch = 1", saveDir, extra)

	nzblnk := fmt.Sprintf("nzblnk:?t=Integration.Title&h=integ-needle&g=alt.binaries.test&d=%d", unixDate)
	out, code := runIntegration(t, cfgPath, nzblnk)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\n%s", code, out)
	}
	nzb := assertSavedNzb(t, saveDir)
	if !strings.Contains(nzb, "integ-needle") {
		t.Fatalf("saved NZB does not contain the needle:\n%s", nzb)
	}
}

// TestIntegrationDirectsearchCacheHit runs the directsearch pipeline twice
// with the cache enabled: the second run must serve the whole scan range from
// the local cache and only fetch the small boundary-probe overviews over NNTP.
func TestIntegrationDirectsearchCacheHit(t *testing.T) {

	// the boundaries scanner always fetches two small overview windows
	// (first/last message date averaging) - only the bulk message scan is
	// cached, so assert on total messages fetched, not request count
	var overMessages atomic.Int64
	server := newFakeNNTPServer(t, func(first, last int) []string {
		overMessages.Add(int64(last - first + 1))
		var lines []string
		for i := first; i <= last; i++ {
			subject := fmt.Sprintf("noise-%d [01/01] - \"noise-%d.part01.rar\" yEnc (1/1)", i, i)
			if i == 150 {
				subject = "integ-needle [01/01] - \"integ-needle-150.part01.rar\" yEnc (1/1)"
			}
			lines = append(lines, overviewLine(i, subject, "poster@example.com", fmt.Sprintf("<%d@x>", i), 100))
		}
		return lines
	})
	probe := overviewLine(1, "x", "y", "z", 1)
	parsed, _ := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", strings.Split(probe, "\t")[3])
	unixDate := parsed.Unix()

	saveDir := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "cache")
	extra := fmt.Sprintf(`[DIRECTSEARCH]
host = "127.0.0.1"
port = %d
ssl = false
skip_check = true
username = "u"
password = "p"
connections = 4
hours = 24
forward_hours = 24
step = 50
overview_timeout = 10
overview_retries = 1
use_ansi_codes = false
show_counter = false
one_instance_only = false
boundaries_scanner_step = 20
boundaries_scanner_tolerance = 60
cache = true
cache_path = "%s"
cache_size = 100
`, server.port(), filepath.ToSlash(cacheDir))
	cfgPath := writeIntegrationConfig(t, "directsearch = 1", saveDir, extra)
	nzblnk := fmt.Sprintf("nzblnk:?t=Integration.Title&h=integ-needle&g=alt.binaries.test&d=%d", unixDate)

	out, code := runIntegration(t, cfgPath, nzblnk)
	if code != 0 {
		t.Fatalf("first run failed with %d\n%s", code, out)
	}
	firstRunMessages := overMessages.Load()
	if firstRunMessages < 1000 {
		t.Fatalf("expected the message scan to fetch 1000 overviews, got %d\n%s", firstRunMessages, out)
	}
	if len(listCacheIntervals(cacheGroupDir(cacheDir, "alt.binaries.test"))) == 0 {
		t.Fatal("first run did not populate the cache")
	}

	overMessages.Store(0)
	out, code = runIntegration(t, cfgPath, nzblnk)
	if code != 0 {
		t.Fatalf("second run failed with %d\n%s", code, out)
	}
	if overMessages.Load() >= firstRunMessages {
		t.Fatalf("cached run fetched %d overviews - expected fewer than the uncached %d\n%s",
			overMessages.Load(), firstRunMessages, out)
	}
	if !strings.Contains(out, "served from local cache") {
		t.Fatalf("expected cache hit message in output:\n%s", out)
	}
	assertSavedNzb(t, saveDir)
}
