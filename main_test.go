package main

import (
	"sort"
	"testing"
)

func TestDiffNames_BasicRemoval(t *testing.T) {
	before := map[string]bool{"nginx": true, "whoami": true, "redis": true}
	after := map[string]bool{"nginx": true, "redis": true}

	removed := diffNames(before, after)

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
	if removed[0] != "whoami" {
		t.Errorf("removed[0] = %q, want %q", removed[0], "whoami")
	}
}

func TestDiffNames_NoRemovals(t *testing.T) {
	before := map[string]bool{"nginx": true, "redis": true}
	after := map[string]bool{"nginx": true, "redis": true, "whoami": true}

	removed := diffNames(before, after)

	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d: %v", len(removed), removed)
	}
}

func TestDiffNames_AllRemoved(t *testing.T) {
	before := map[string]bool{"nginx": true, "redis": true}
	after := map[string]bool{}

	removed := diffNames(before, after)

	sort.Strings(removed)
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
	if removed[0] != "nginx" || removed[1] != "redis" {
		t.Errorf("removed = %v, want [nginx redis]", removed)
	}
}

func TestDiffNames_BeforeNil_FailSafe(t *testing.T) {
	after := map[string]bool{"nginx": true}

	removed := diffNames(nil, after)

	if removed != nil {
		t.Errorf("expected nil when before is nil, got %v", removed)
	}
}

func TestDiffNames_AfterNil_FailSafe(t *testing.T) {
	before := map[string]bool{"nginx": true}

	removed := diffNames(before, nil)

	if removed != nil {
		t.Errorf("expected nil when after is nil, got %v", removed)
	}
}

func TestDiffNames_BothNil_FailSafe(t *testing.T) {
	removed := diffNames(nil, nil)

	if removed != nil {
		t.Errorf("expected nil when both are nil, got %v", removed)
	}
}

func TestDiffNames_BothEmpty(t *testing.T) {
	before := map[string]bool{}
	after := map[string]bool{}

	removed := diffNames(before, after)

	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
}

func TestDiffNames_CompleteSwap(t *testing.T) {
	before := map[string]bool{"nginx": true, "redis": true}
	after := map[string]bool{"postgres": true, "whoami": true}

	removed := diffNames(before, after)

	sort.Strings(removed)
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
	if removed[0] != "nginx" || removed[1] != "redis" {
		t.Errorf("removed = %v, want [nginx redis]", removed)
	}
}
