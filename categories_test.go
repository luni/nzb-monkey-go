package main

import "testing"

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
