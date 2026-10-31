package main

import (
	"bufio"
	"strings"
	"testing"
)

// with multiple configured targets the first one supporting categories
// must be selected (regression test: targets["A,B"] never matched a key)
func TestFirstCategoryTarget(t *testing.T) {
	conf.General.Targets = []string{"EXECUTE", "SABNZBD", "NZBGET"}

	target, ok := firstCategoryTarget()
	if !ok {
		t.Fatal("expected a target with category support")
	}
	if target.name != "SABnzbd" {
		t.Errorf("target = %q, want SABnzbd (EXECUTE has no getCategories)", target.name)
	}
}

func TestFirstCategoryTargetSingle(t *testing.T) {
	conf.General.Targets = []string{"NZBGET"}

	target, ok := firstCategoryTarget()
	if !ok || target.name != "NZBGet" {
		t.Errorf("target = %q/%v, want NZBGet/true", target.name, ok)
	}
}

// no configured target supports categories
func TestFirstCategoryTargetNone(t *testing.T) {
	conf.General.Targets = []string{"EXECUTE", "SYNOLOGYDLS"}

	if _, ok := firstCategoryTarget(); ok {
		t.Error("expected no category-capable target")
	}
}

func TestFirstCategoryTargetEmpty(t *testing.T) {
	conf.General.Targets = nil

	if _, ok := firstCategoryTarget(); ok {
		t.Error("expected no target for empty config")
	}
}

// in "auto" mode the first matching category regex wins;
// unmatched and invalid regexes fall back to ""
func TestCheckCategoriesAuto(t *testing.T) {
	conf.General.Categorize = "auto"
	conf.Categories = []CategorySettings{
		{name: "movies", regex: "(x264|bluray)"},
		{name: "series", regex: "s\\d+e\\d+"},
	}

	args.Title = "Some.Movie.2024.BLURAY.x264"
	if got := checkCategories(); got != "movies" {
		t.Errorf("checkCategories() = %q, want %q", got, "movies")
	}

	args.Title = "Some.Show.S01E02"
	if got := checkCategories(); got != "series" {
		t.Errorf("checkCategories() = %q, want %q", got, "series")
	}

	args.Title = "nothing-matching"
	if got := checkCategories(); got != "" {
		t.Errorf("checkCategories() = %q, want empty", got)
	}
}

// an invalid regex must be skipped, not abort the whole check
func TestCheckCategoriesInvalidRegex(t *testing.T) {
	conf.General.Categorize = "auto"
	conf.Categories = []CategorySettings{
		{name: "broken", regex: "([unclosed"},
		{name: "movies", regex: "x264"},
	}
	args.Title = "Some.Movie.x264"

	if got := checkCategories(); got != "movies" {
		t.Errorf("checkCategories() = %q, want %q", got, "movies")
	}
}

// consecutive reads must not lose input already buffered by a previous
// call (regression test: a fresh bufio.Reader per call discarded buffered
// input, e.g. when pasting multiple lines at once)
func TestInputReaderBufferedInput(t *testing.T) {
	old := stdinReader
	defer func() { stdinReader = old }()
	stdinReader = bufio.NewReader(strings.NewReader("1\nx\n"))

	if got := inputReader(); got != "1" {
		t.Errorf("inputReader() = %q, want %q", got, "1")
	}
	if got := inputReader(); got != "x" {
		t.Errorf("second inputReader() = %q, want %q", got, "x")
	}
}

func TestInputReaderTrimsSpace(t *testing.T) {
	old := stdinReader
	defer func() { stdinReader = old }()
	stdinReader = bufio.NewReader(strings.NewReader("  2  \r\n"))

	if got := inputReader(); got != "2" {
		t.Errorf("inputReader() = %q, want %q", got, "2")
	}
}
