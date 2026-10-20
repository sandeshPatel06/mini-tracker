package main

import (
	"testing"
	"time"

	"github.com/reak/get-hike/internal/db"
)

func TestGroupCompletedBundlesSkipsActiveWindow(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 7, 30, 0, time.Local)
	logs := []db.LogEntry{
		{ID: 1, Timestamp: time.Date(2026, 10, 19, 10, 0, 5, 0, time.Local)},
		{ID: 2, Timestamp: time.Date(2026, 10, 19, 10, 4, 55, 0, time.Local)},
		{ID: 3, Timestamp: time.Date(2026, 10, 19, 10, 5, 30, 0, time.Local)},
		{ID: 4, Timestamp: time.Date(2026, 10, 19, 10, 6, 0, 0, time.Local)},
		{ID: 5, Timestamp: time.Date(2026, 10, 19, 10, 7, 0, 0, time.Local)},
	}

	bundles := groupCompletedBundles(logs, now)
	if len(bundles) != 1 {
		t.Fatalf("expected 1 completed bundle, got %d: %+v", len(bundles), bundles)
	}
	wantStart := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	if !bundles[0].start.Equal(wantStart) {
		t.Fatalf("bundle start = %v, want %v", bundles[0].start, wantStart)
	}
	if len(bundles[0].logs) != 2 {
		t.Fatalf("bundle logs = %d, want 2 (active 10:05 window must stay pending)", len(bundles[0].logs))
	}
	if bundles[0].logs[0].ID != 1 || bundles[0].logs[1].ID != 2 {
		t.Fatalf("unexpected bundle members: %+v", bundles[0].logs)
	}
}

func TestGroupCompletedBundlesSeparatesWindows(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 20, 0, 0, time.Local)
	logs := []db.LogEntry{
		{ID: 1, Timestamp: time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)},
		{ID: 2, Timestamp: time.Date(2026, 10, 19, 10, 5, 1, 0, time.Local)},
		{ID: 3, Timestamp: time.Date(2026, 10, 19, 10, 9, 59, 0, time.Local)},
	}

	bundles := groupCompletedBundles(logs, now)
	if len(bundles) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(bundles))
	}
	// Newest windows come first.
	if len(bundles[0].logs) != 2 || bundles[0].logs[0].ID != 2 || bundles[0].logs[1].ID != 3 {
		t.Fatalf("first bundle members = %+v, want newest logs 2,3", bundles[0].logs)
	}
	if len(bundles[1].logs) != 1 || bundles[1].logs[0].ID != 1 {
		t.Fatalf("second bundle members = %+v, want oldest log 1", bundles[1].logs)
	}
}

func TestBundleWindowLabel(t *testing.T) {
	start := time.Date(2026, 10, 19, 10, 0, 0, 0, time.Local)
	if got, want := bundleWindowLabel(start), "2026-10-19 10:00-10:05 (5-minute bundle)"; got != want {
		t.Fatalf("bundleWindowLabel = %q, want %q", got, want)
	}
}

func TestGroupCompletedBundlesSeparatesOrgsAndUsers(t *testing.T) {
	now := time.Date(2026, 10, 19, 10, 20, 0, 0, time.Local)
	logs := []db.LogEntry{
		{ID: 1, OrgID: 1, UserID: 10, Timestamp: time.Date(2026, 10, 19, 10, 0, 5, 0, time.Local)},
		{ID: 2, OrgID: 1, UserID: 11, Timestamp: time.Date(2026, 10, 19, 10, 1, 5, 0, time.Local)},
		{ID: 3, OrgID: 2, UserID: 10, Timestamp: time.Date(2026, 10, 19, 10, 2, 5, 0, time.Local)},
	}

	bundles := groupCompletedBundles(logs, now)
	if len(bundles) != 3 {
		t.Fatalf("expected 3 bundles (one per org/user), got %d", len(bundles))
	}
	for _, bundle := range bundles {
		if len(bundle.logs) != 1 {
			t.Fatalf("bundle for org=%d user=%d has %d logs, want 1", bundle.logs[0].OrgID, bundle.logs[0].UserID, len(bundle.logs))
		}
	}
}
