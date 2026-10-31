package main

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tensai75/nzbparser"
)

// an empty NZB must be skipped, not crash on Files[0] (regression test)
func TestProcessResultEmptyNzb(t *testing.T) {
	results = nil
	args = struct{ Args }{Args: Args{Title: "test"}}

	processResult(&nzbparser.Nzb{}, "TestEngine")
	processResult(nil, "TestEngine")

	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

// TotalSegments == 0 must not produce NaN percentage (regression test for
// the division-by-zero guard)
func TestProcessResultZeroTotalSegments(t *testing.T) {
	results = nil
	args = struct{ Args }{Args: Args{Title: "test"}}
	conf.Nzbcheck = NZBcheck{BestNZB: true, MaxMissingFiles: 5, MaxMissingSegmentsPercent: 100}

	nzb := &nzbparser.Nzb{
		Files:         nzbparser.NzbFiles{{Subject: "test file"}},
		TotalFiles:    2, // FilesMissing = 1 so the result is appended, not pushed to targets
		TotalSegments: 0,
		Segments:      0,
	}
	processResult(nzb, "TestEngine")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if math.IsNaN(results[0].SegmentsMissingPercent) {
		t.Error("SegmentsMissingPercent is NaN, want 0")
	}
	if results[0].SegmentsMissingPercent != 0 {
		t.Errorf("SegmentsMissingPercent = %v, want 0", results[0].SegmentsMissingPercent)
	}
	if !results[0].SegmentsComplete {
		t.Error("SegmentsComplete = false, want true for 0% missing")
	}
}

// normal percentage calculation still works
func TestProcessResultMissingPercent(t *testing.T) {
	results = nil
	args = struct{ Args }{Args: Args{Title: "test"}}
	conf.Nzbcheck = NZBcheck{BestNZB: true, MaxMissingFiles: 5, MaxMissingSegmentsPercent: 100}

	nzb := &nzbparser.Nzb{
		Files:         nzbparser.NzbFiles{{Subject: "test file"}},
		TotalFiles:    2,
		TotalSegments: 4,
		Segments:      3,
	}
	processResult(nzb, "TestEngine")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SegmentsMissingPercent != 25 {
		t.Errorf("SegmentsMissingPercent = %v, want 25", results[0].SegmentsMissingPercent)
	}
	if results[0].SegmentsMissing != 1 {
		t.Errorf("SegmentsMissing = %d, want 1", results[0].SegmentsMissing)
	}
}

// end-to-end: processFoundNzb must serialize the NZB, push it to the
// EXECUTE target (file written to disk) and exit 0; verified via
// subprocess because exit() terminates via os.Exit
func TestProcessFoundNzbExecute(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=TestProcessFoundNzbHelperProcess")
	cmd.Env = append(os.Environ(),
		"GO_TEST_HELPER=3",
		"TEST_SAVEDIR="+dir,
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected exit code 0, got %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in save dir, got %d", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `meta type="title"`) || !strings.Contains(string(data), "e2e title") {
		t.Errorf("written NZB does not contain the title meta: %s", data)
	}
}

// helper process for the end-to-end test; not a real test when run by the parent
func TestProcessFoundNzbHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_HELPER") != "3" {
		return
	}
	conf.General.Targets = []string{"EXECUTE"}
	conf.General.Success_wait_time = 1 // keep the exit countdown short
	conf.Execute = Execute{
		Nzbsavepath: os.Getenv("TEST_SAVEDIR"),
		Dontexecute: true,
	}
	args.Title = "e2e title"

	nzb := &nzbparser.Nzb{
		Files:         nzbparser.NzbFiles{{Subject: "test file"}},
		TotalFiles:    1,
		TotalSegments: 1,
		Segments:      1,
	}
	processFoundNzb(&Result{
		SearchEngine:     "TestEngine",
		Nzb:              nzb,
		FilesComplete:    true,
		SegmentsComplete: true,
	})
	os.Exit(9) // processFoundNzb must never return
}
