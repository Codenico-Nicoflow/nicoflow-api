package task

import (
	"testing"
	"time"
)

var tsNow = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC) // a Monday noon

func schedTask(id string, dayOffset int, rollsOver bool) Task {
	s := tsNow.AddDate(0, 0, dayOffset).Format(scheduledForLayout)
	return Task{ID: id, Status: "active", RollsOver: rollsOver, ScheduledFor: &s}
}

func inBucket(items []TaskView, id string) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func TestBucketTimeSpread(t *testing.T) {
	candidates := []Task{
		schedTask("sched-today", 0, true),
		schedTask("sched-tomorrow", 1, true),
		schedTask("sched-thisweek", 4, true),
		schedTask("rolled-over", -2, true),    // past + rollsOver → today
		schedTask("dropped", -2, false),       // past + no rollsOver → nowhere
		schedTask("far-future", 30, true),     // beyond this week → nowhere
		{ID: "unscheduled", Status: "active"}, // no schedule → nowhere
	}

	got := bucketTimeSpread(candidates, tsNow)

	// today
	for _, id := range []string{"sched-today", "rolled-over"} {
		if !inBucket(got.Today, id) {
			t.Errorf("%s should be in today", id)
		}
	}
	// tomorrow
	for _, id := range []string{"sched-tomorrow"} {
		if !inBucket(got.Tomorrow, id) {
			t.Errorf("%s should be in tomorrow", id)
		}
	}
	// this week
	if !inBucket(got.ThisWeek, "sched-thisweek") {
		t.Errorf("sched-thisweek should be in thisWeek")
	}
	// excluded from all buckets
	for _, id := range []string{"dropped", "far-future", "unscheduled"} {
		if inBucket(got.Today, id) || inBucket(got.Tomorrow, id) || inBucket(got.ThisWeek, id) {
			t.Errorf("%s should not be in any bucket", id)
		}
	}
}

func TestBucketTimeSpread_RollForwardVsDrop(t *testing.T) {
	rolls := bucketTimeSpread([]Task{schedTask("a", -1, true)}, tsNow)
	if !inBucket(rolls.Today, "a") {
		t.Error("past rollsOver task should carry to today")
	}
	drops := bucketTimeSpread([]Task{schedTask("b", -1, false)}, tsNow)
	if len(drops.Today)+len(drops.Tomorrow)+len(drops.ThisWeek) != 0 {
		t.Error("past non-rollsOver task should drop off entirely")
	}
}

func TestBucketTimeSpread_EmptyBucketsNonNil(t *testing.T) {
	got := bucketTimeSpread(nil, tsNow)
	if got.Today == nil || got.Tomorrow == nil || got.ThisWeek == nil {
		t.Error("buckets should serialize as [] not null")
	}
}
