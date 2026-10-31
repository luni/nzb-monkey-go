package main

import (
	"math"
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
