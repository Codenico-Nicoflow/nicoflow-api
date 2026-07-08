package task

import (
	"testing"
	"time"
)

// parseTimezone: empty → UTC, valid IANA → that zone, unknown → error (400 caller).
func TestParseTimezone(t *testing.T) {
	tests := []struct {
		name    string
		tz      string
		wantErr bool
		want    string // expected loc.String() when no error
	}{
		{name: "empty defaults to UTC", tz: "", want: "UTC"},
		{name: "valid IANA zone", tz: "Asia/Jerusalem", want: "Asia/Jerusalem"},
		{name: "UTC literal", tz: "UTC", want: "UTC"},
		{name: "unknown zone errors", tz: "Not/AZone", wantErr: true},
		{name: "garbage errors", tz: "!!!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := parseTimezone(tt.tz)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseTimezone(%q) = nil error, want error", tt.tz)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTimezone(%q) unexpected error: %v", tt.tz, err)
			}
			if loc.String() != tt.want {
				t.Errorf("parseTimezone(%q) = %q, want %q", tt.tz, loc.String(), tt.want)
			}
		})
	}
}

// The same instant buckets into a different day depending on the zone: a task
// scheduled for the local calendar day is "today" in a zone that has already
// rolled over, but not yet in UTC. This is what wiring `now.In(loc)` buys us.
func TestBucketTimeSpread_ZoneShiftsToday(t *testing.T) {
	// 2026-06-15 22:00 UTC = 2026-06-16 01:00 in Asia/Jerusalem (UTC+3).
	instant := time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC)
	jer, err := time.LoadLocation("Asia/Jerusalem")
	if err != nil {
		t.Fatalf("load zone: %v", err)
	}

	sched16 := "2026-06-16"
	task := Task{ID: "t", Status: "active", ScheduledFor: &sched16}

	// In UTC the 16th is still tomorrow.
	utc := bucketTimeSpread([]Task{task}, instant.In(time.UTC))
	if !inBucket(utc.Tomorrow, "t") {
		t.Error("in UTC the 16th should bucket as tomorrow")
	}
	if inBucket(utc.Today, "t") {
		t.Error("in UTC the 16th should not be today")
	}

	// In Jerusalem it's already the 16th, so the task is today.
	local := bucketTimeSpread([]Task{task}, instant.In(jer))
	if !inBucket(local.Today, "t") {
		t.Error("in Asia/Jerusalem the 16th should bucket as today")
	}
}
