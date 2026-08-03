package google

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

func TestEventItem_ToDomain(t *testing.T) {
	tests := []struct {
		name       string
		item       eventItem
		wantOK     bool
		wantAllDay bool
		wantTitle  string
		wantStart  string
	}{
		{
			name: "timed event keeps its offset",
			item: eventItem{
				ID: "evt-1", Summary: "Standup", Status: "confirmed",
				Start: eventDateTime{DateTime: "2026-08-03T09:00:00+03:00"},
				End:   eventDateTime{DateTime: "2026-08-03T09:30:00+03:00"},
			},
			wantOK: true, wantTitle: "Standup",
			// Parsed as an absolute instant — 09:00+03:00 is 06:00Z.
			wantStart: "2026-08-03T06:00:00Z",
		},
		{
			name: "all-day event is a floating date",
			item: eventItem{
				ID: "evt-2", Summary: "Birthday", Status: "confirmed",
				Start: eventDateTime{Date: "2026-08-03"},
				End:   eventDateTime{Date: "2026-08-04"},
			},
			wantOK: true, wantAllDay: true, wantTitle: "Birthday",
			wantStart: "2026-08-03T00:00:00Z",
		},
		{
			name: "private event with no title gets an honest label",
			item: eventItem{
				ID: "evt-3", Status: "confirmed",
				Start: eventDateTime{DateTime: "2026-08-03T09:00:00Z"},
				End:   eventDateTime{DateTime: "2026-08-03T10:00:00Z"},
			},
			wantOK: true, wantTitle: "(no title)",
			wantStart: "2026-08-03T09:00:00Z",
		},
		{
			name: "cancelled event is skipped",
			item: eventItem{
				ID: "evt-4", Status: "cancelled",
				Start: eventDateTime{DateTime: "2026-08-03T09:00:00Z"},
			},
			wantOK: false,
		},
		{
			name:   "event with no start is skipped",
			item:   eventItem{ID: "evt-5", Status: "confirmed"},
			wantOK: false,
		},
		{
			name: "unparseable start is skipped",
			item: eventItem{
				ID: "evt-6", Status: "confirmed",
				Start: eventDateTime{DateTime: "not-a-timestamp"},
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.item.toDomain("primary")
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.AllDay != tt.wantAllDay {
				t.Errorf("allDay = %v, want %v", got.AllDay, tt.wantAllDay)
			}
			if start := got.Start.UTC().Format(time.RFC3339); start != tt.wantStart {
				t.Errorf("start = %q, want %q", start, tt.wantStart)
			}
			if got.CalendarID != "primary" {
				t.Errorf("calendarID = %q", got.CalendarID)
			}
		})
	}
}

func TestEventItem_ToDomain_Detail(t *testing.T) {
	got, ok := eventItem{
		ID: "evt-1", Summary: "Standup", Status: "confirmed",
		Location:    "Room 4",
		Description: "Agenda:<br><b>ship it</b>",
		Organizer:   eventPerson{Email: "lead@example.com", DisplayName: "Lead"},
		Attendees: []eventPerson{
			{Email: "lead@example.com", ResponseStatus: "accepted"},
			{Email: "me@example.com", Self: true, ResponseStatus: "tentative"},
		},
		Start: eventDateTime{DateTime: "2026-08-03T09:00:00Z"},
		End:   eventDateTime{DateTime: "2026-08-03T09:30:00Z"},
	}.toDomain("primary")

	if !ok {
		t.Fatal("event skipped")
	}
	if got.Location != "Room 4" {
		t.Errorf("location = %q", got.Location)
	}
	if got.Description != "Agenda: ship it" {
		t.Errorf("description = %q, want the HTML flattened", got.Description)
	}
	if got.Organizer != "Lead" {
		t.Errorf("organizer = %q, want the display name", got.Organizer)
	}
	if got.AttendeeCount != 2 {
		t.Errorf("attendeeCount = %d, want 2", got.AttendeeCount)
	}
	// The viewer's own RSVP, not the organizer's.
	if got.Response != googlecal.ResponseTentative {
		t.Errorf("response = %q, want tentative", got.Response)
	}
}

func TestEventItem_ToDomain_DetailDefaults(t *testing.T) {
	got, ok := eventItem{
		ID: "evt-2", Summary: "Solo work", Status: "confirmed",
		Organizer: eventPerson{Email: "me@example.com"},
		Start:     eventDateTime{DateTime: "2026-08-03T09:00:00Z"},
		End:       eventDateTime{DateTime: "2026-08-03T10:00:00Z"},
	}.toDomain("primary")

	if !ok {
		t.Fatal("event skipped")
	}
	// No display name from Google leaves only the address to identify them by.
	if got.Organizer != "me@example.com" {
		t.Errorf("organizer = %q, want the email fallback", got.Organizer)
	}
	// No attendee list means this is a personal entry, not an unanswered invite.
	if got.Response != googlecal.ResponseNone {
		t.Errorf("response = %q, want empty", got.Response)
	}
	if got.AttendeeCount != 0 {
		t.Errorf("attendeeCount = %d, want 0", got.AttendeeCount)
	}
}

// A timed event missing its end still places on the grid rather than vanishing.
func TestEventItem_ToDomain_MissingEndFallsBackToStart(t *testing.T) {
	got, ok := eventItem{
		ID: "evt-1", Summary: "Ping", Status: "confirmed",
		Start: eventDateTime{DateTime: "2026-08-03T09:00:00Z"},
	}.toDomain("primary")

	if !ok {
		t.Fatal("event skipped")
	}
	if !got.End.Equal(got.Start) {
		t.Errorf("end = %v, want it to equal start", got.End)
	}
}

// invalid_grant is the one terminal failure — everything else must stay
// retryable, or a transient blip would disconnect users permanently.
func TestIsInvalidGrant(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"invalid_grant on 400", http.StatusBadRequest, `{"error":"invalid_grant"}`, true},
		{"invalid_grant on 401", http.StatusUnauthorized, `{"error":"invalid_grant"}`, true},
		{"other token error is retryable", http.StatusBadRequest, `{"error":"invalid_request"}`, false},
		{"rate limit is retryable", http.StatusTooManyRequests, `{"error":"rateLimitExceeded"}`, false},
		{"server error is retryable", http.StatusInternalServerError, `{}`, false},
		{"unparseable 400 is treated as terminal", http.StatusBadRequest, `<html>nope`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInvalidGrant(tt.status, []byte(tt.body)); got != tt.want {
				t.Errorf("isInvalidGrant(%d, %q) = %v, want %v", tt.status, tt.body, got, tt.want)
			}
		})
	}
}

// A disabled client must fail with a typed error rather than attempting a call.
func TestClient_Disabled(t *testing.T) {
	client := New("", "", "")

	if client.Enabled() {
		t.Fatal("client with no credentials reports enabled")
	}

	if _, err := client.ListEvents(t.Context(), googlecal.Secret("x"), "primary", time.Now(), time.Now()); !errors.Is(err, googlecal.ErrOAuthDisabled) {
		t.Errorf("ListEvents error = %v, want ErrOAuthDisabled", err)
	}
	if _, err := client.ListCalendars(t.Context(), googlecal.Secret("x")); !errors.Is(err, googlecal.ErrOAuthDisabled) {
		t.Errorf("ListCalendars error = %v, want ErrOAuthDisabled", err)
	}
}
