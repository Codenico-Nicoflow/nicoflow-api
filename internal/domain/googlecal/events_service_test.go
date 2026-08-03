package googlecal_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

// --- mocks -------------------------------------------------------------------

// mockCalendar records what it was asked for so tests can assert on call counts
// — the only way to prove the cache actually prevented a second Google call.
type mockCalendar struct {
	disabled bool
	// eventsByCalendar lets a test give one calendar events and another an error.
	eventsByCalendar map[string][]googlecal.CalendarEvent
	errByCalendar    map[string]error
	err              error
	calls            int
	calendars        []googlecal.Calendar
	lastFrom         time.Time
	lastTo           time.Time
}

func (m *mockCalendar) Enabled() bool { return !m.disabled }

func (m *mockCalendar) ListEvents(_ context.Context, _ googlecal.Secret, calendarID string, from, to time.Time) ([]googlecal.CalendarEvent, error) {
	m.calls++
	m.lastFrom, m.lastTo = from, to
	if err, ok := m.errByCalendar[calendarID]; ok {
		return nil, err
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.eventsByCalendar[calendarID], nil
}

func (m *mockCalendar) ListCalendars(_ context.Context, _ googlecal.Secret) ([]googlecal.Calendar, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.calendars, nil
}

// eventsRepo is a Repository stub for the events service.
type eventsRepo struct {
	conn        googlecal.Connection
	getErr      error
	timezone    string
	deleteCalls int
	setErrCalls int
	lastSetErr  *string
}

func (r *eventsRepo) Get(_ context.Context, _ string) (googlecal.Connection, error) {
	if r.getErr != nil {
		return googlecal.Connection{}, r.getErr
	}
	return r.conn, nil
}

func (r *eventsRepo) Upsert(_ context.Context, c googlecal.Connection) (googlecal.Connection, error) {
	return c, nil
}

func (r *eventsRepo) UpdateSelectedCalendars(_ context.Context, _ string, ids []string) (googlecal.Connection, error) {
	r.conn.SelectedCalendarIDs = ids
	return r.conn, nil
}

func (r *eventsRepo) SetError(_ context.Context, _ string, message *string) error {
	r.setErrCalls++
	r.lastSetErr = message
	return nil
}

func (r *eventsRepo) Delete(_ context.Context, _ string) error {
	r.deleteCalls++
	return nil
}

func (r *eventsRepo) UserTimezone(_ context.Context, _ string) (string, error) {
	if r.timezone == "" {
		return "UTC", nil
	}
	return r.timezone, nil
}

func connectedRepo(calendarIDs ...string) *eventsRepo {
	return &eventsRepo{conn: googlecal.Connection{
		UserID:              "u1",
		RefreshToken:        googlecal.Secret("refresh-token"),
		GoogleAccountEmail:  "user@example.com",
		SelectedCalendarIDs: calendarIDs,
	}}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// --- tests -------------------------------------------------------------------

func TestEventsService_List_MapsEvents(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{eventsByCalendar: map[string][]googlecal.CalendarEvent{
		"primary": {{
			ID:         "evt-1",
			Title:      "Standup",
			CalendarID: "primary",
			HTMLLink:   "https://calendar.google.com/evt-1",
			Start:      mustTime(t, "2026-08-03T09:00:00Z"),
			End:        mustTime(t, "2026-08-03T09:30:00Z"),
		}},
	}}

	resp, err := googlecal.NewEventsService(repo, client).List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != googlecal.StatusOK {
		t.Errorf("status = %q, want %q", resp.Status, googlecal.StatusOK)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(resp.Events))
	}
	got := resp.Events[0]
	if got.ID != "evt-1" || got.Title != "Standup" || got.CalendarID != "primary" {
		t.Errorf("unexpected event mapping: %+v", got)
	}
	if got.HTMLLink != "https://calendar.google.com/evt-1" {
		t.Errorf("htmlLink = %q", got.HTMLLink)
	}
	if got.AllDay {
		t.Error("timed event marked all-day")
	}
}

// Google failing must never surface as a 5xx — the calendar view depends on
// this endpoint answering.
func TestEventsService_List_ContainsFailures(t *testing.T) {
	tests := []struct {
		name       string
		repo       *eventsRepo
		client     *mockCalendar
		wantStatus googlecal.GoogleStatus
		wantDelete int
	}{
		{
			name:       "google 5xx is contained",
			repo:       connectedRepo("primary"),
			client:     &mockCalendar{err: googlecal.ErrCalendarUnavailable},
			wantStatus: googlecal.StatusError,
		},
		{
			name:       "invalid_grant disconnects and clears the token",
			repo:       connectedRepo("primary"),
			client:     &mockCalendar{err: googlecal.ErrCalendarUnauthorized},
			wantStatus: googlecal.StatusDisconnected,
			wantDelete: 1,
		},
		{
			name: "never connected reads as disconnected",
			repo: &eventsRepo{getErr: apperror.New(
				http.StatusConflict, apperror.ErrGoogleNotConnected, "not connected")},
			client:     &mockCalendar{},
			wantStatus: googlecal.StatusDisconnected,
		},
		{
			name: "undecryptable token reads as disconnected",
			repo: &eventsRepo{getErr: apperror.New(
				http.StatusBadGateway, apperror.ErrGoogleAuthFailed, "cannot decrypt")},
			client:     &mockCalendar{},
			wantStatus: googlecal.StatusDisconnected,
		},
		{
			name:       "unconfigured integration reads as disconnected",
			repo:       connectedRepo("primary"),
			client:     &mockCalendar{disabled: true},
			wantStatus: googlecal.StatusDisconnected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := googlecal.NewEventsService(tt.repo, tt.client).
				List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)

			if err != nil {
				t.Fatalf("returned an error instead of a status: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", resp.Status, tt.wantStatus)
			}
			if resp.Events == nil {
				t.Error("events is nil; must marshal as []")
			}
			if len(resp.Events) != 0 {
				t.Errorf("events = %d, want 0 on failure", len(resp.Events))
			}
			if tt.repo.deleteCalls != tt.wantDelete {
				t.Errorf("delete calls = %d, want %d", tt.repo.deleteCalls, tt.wantDelete)
			}
		})
	}
}

// invalid_grant must not spin: one attempt, then disconnect.
func TestEventsService_List_InvalidGrantDoesNotRetry(t *testing.T) {
	repo := connectedRepo("primary", "team@example.com")
	client := &mockCalendar{err: googlecal.ErrCalendarUnauthorized}

	resp, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != googlecal.StatusDisconnected {
		t.Errorf("status = %q, want disconnected", resp.Status)
	}
	// Stops at the first calendar rather than trying the second with a token
	// Google has already rejected.
	if client.calls != 1 {
		t.Errorf("google calls = %d, want 1 (no retry, no fan-out)", client.calls)
	}
	if repo.deleteCalls != 1 {
		t.Errorf("delete calls = %d, want 1", repo.deleteCalls)
	}
}

func TestEventsService_List_CacheAbsorbsRepeats(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{eventsByCalendar: map[string][]googlecal.CalendarEvent{
		"primary": {{ID: "evt-1", CalendarID: "primary", Start: mustTime(t, "2026-08-03T09:00:00Z"), End: mustTime(t, "2026-08-03T10:00:00Z")}},
	}}
	svc := googlecal.NewEventsService(repo, client)

	for i := range 3 {
		if _, err := svc.List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	if client.calls != 1 {
		t.Errorf("google calls = %d, want 1 (cache should absorb repeats)", client.calls)
	}
}

func TestEventsService_List_ExplicitRefreshBypassesCache(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{}
	svc := googlecal.NewEventsService(repo, client)

	if _, err := svc.List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
		t.Fatalf("warm: %v", err)
	}
	if _, err := svc.List(context.Background(), "u1", "2026-08-03", "2026-08-03", true); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if client.calls != 2 {
		t.Errorf("google calls = %d, want 2 (refresh must bypass the cache)", client.calls)
	}
}

// Different ranges are different cache entries — a cached day must not satisfy
// a request for a week.
func TestEventsService_List_CacheKeyedByRange(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{}
	svc := googlecal.NewEventsService(repo, client)

	if _, err := svc.List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.List(context.Background(), "u1", "2026-08-03", "2026-08-09", false); err != nil {
		t.Fatalf("second: %v", err)
	}

	if client.calls != 2 {
		t.Errorf("google calls = %d, want 2 (distinct ranges are distinct keys)", client.calls)
	}
}

// A failed fetch must not be cached: a blip would otherwise force minutes of
// emptiness after Google recovers.
func TestEventsService_List_FailuresAreNotCached(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{err: googlecal.ErrCalendarUnavailable}
	svc := googlecal.NewEventsService(repo, client)

	for range 2 {
		if _, err := svc.List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if client.calls != 2 {
		t.Errorf("google calls = %d, want 2 (a failure must not be cached)", client.calls)
	}
}

// A deleted or unshared calendar drops out; the rest still render.
func TestEventsService_List_TolerentOfStaleCalendar(t *testing.T) {
	repo := connectedRepo("primary", "deleted@example.com")
	client := &mockCalendar{
		eventsByCalendar: map[string][]googlecal.CalendarEvent{
			"primary": {{ID: "evt-1", CalendarID: "primary", Start: mustTime(t, "2026-08-03T09:00:00Z"), End: mustTime(t, "2026-08-03T10:00:00Z")}},
		},
		errByCalendar: map[string]error{"deleted@example.com": googlecal.ErrCalendarNotFound},
	}

	resp, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != googlecal.StatusOK {
		t.Errorf("status = %q, want ok — one stale calendar must not fail the fetch", resp.Status)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("events = %d, want 1 from the surviving calendar", len(resp.Events))
	}
}

// Events are converted with a zone-aware offset, which is what keeps the local
// hour correct on either side of a DST transition.
func TestEventsService_List_DSTSafeConversion(t *testing.T) {
	// Europe/Berlin leaves DST on 2026-10-25: 02:00 CEST (+02:00) → 01:00 CET.
	repo := connectedRepo("primary")
	repo.timezone = "Europe/Berlin"
	client := &mockCalendar{eventsByCalendar: map[string][]googlecal.CalendarEvent{
		"primary": {
			{ID: "before", CalendarID: "primary", Start: mustTime(t, "2026-10-24T08:00:00Z"), End: mustTime(t, "2026-10-24T09:00:00Z")},
			{ID: "after", CalendarID: "primary", Start: mustTime(t, "2026-10-26T08:00:00Z"), End: mustTime(t, "2026-10-26T09:00:00Z")},
		},
	}}

	resp, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-10-24", "2026-10-26", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Fatalf("events = %d, want 2", len(resp.Events))
	}

	// Same UTC instant, different offsets: +02:00 before the switch, +01:00 after.
	// A naive fixed-offset conversion returns the same local hour for both, which
	// is the bug this asserts against.
	if want := "2026-10-24T10:00:00+02:00"; resp.Events[0].Start != want {
		t.Errorf("pre-DST start = %q, want %q", resp.Events[0].Start, want)
	}
	if want := "2026-10-26T09:00:00+01:00"; resp.Events[1].Start != want {
		t.Errorf("post-DST start = %q, want %q", resp.Events[1].Start, want)
	}
}

// All-day events are dates, not instants — giving them a time would drift them
// across the day boundary for anyone in another zone.
func TestEventsService_List_AllDayRendersAsDate(t *testing.T) {
	repo := connectedRepo("primary")
	repo.timezone = "Asia/Jerusalem"
	client := &mockCalendar{eventsByCalendar: map[string][]googlecal.CalendarEvent{
		"primary": {{
			ID: "birthday", CalendarID: "primary", AllDay: true,
			Start: mustTime(t, "2026-08-03T00:00:00Z"),
			End:   mustTime(t, "2026-08-04T00:00:00Z"),
		}},
	}}

	resp, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := resp.Events[0]
	if !got.AllDay {
		t.Error("allDay = false, want true")
	}
	if got.Start != "2026-08-03" {
		t.Errorf("start = %q, want a plain date", got.Start)
	}
}

// The range is resolved in the user's zone, not UTC — otherwise the first hours
// of the day are silently missing for anyone east of UTC.
func TestEventsService_List_RangeResolvedInUserZone(t *testing.T) {
	repo := connectedRepo("primary")
	repo.timezone = "Asia/Jerusalem" // UTC+3 in August
	client := &mockCalendar{}

	if _, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := "2026-08-02T21:00:00Z"; client.lastFrom.UTC().Format(time.RFC3339) != want {
		t.Errorf("from = %q, want %q (local midnight)", client.lastFrom.UTC().Format(time.RFC3339), want)
	}
	// Exclusive upper bound: local midnight starting the following day.
	if want := "2026-08-03T21:00:00Z"; client.lastTo.UTC().Format(time.RFC3339) != want {
		t.Errorf("to = %q, want %q (exclusive end)", client.lastTo.UTC().Format(time.RFC3339), want)
	}
}

func TestEventsService_List_RejectsBadRange(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
	}{
		{"missing both", "", ""},
		{"missing to", "2026-08-03", ""},
		{"malformed from", "03-08-2026", "2026-08-03"},
		{"malformed to", "2026-08-03", "not-a-date"},
		{"inverted", "2026-08-10", "2026-08-03"},
		{"span over 62 days", "2026-01-01", "2026-12-31"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &mockCalendar{}
			_, err := googlecal.NewEventsService(connectedRepo("primary"), client).
				List(context.Background(), "u1", tt.from, tt.to, false)

			var appErr *apperror.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("error = %v, want a typed AppError", err)
			}
			if appErr.Code != apperror.ErrInvalidInput {
				t.Errorf("code = %q, want %q", appErr.Code, apperror.ErrInvalidInput)
			}
			if appErr.Status != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", appErr.Status)
			}
			if client.calls != 0 {
				t.Error("google was called despite an invalid range")
			}
		})
	}
}

// A connection with no selection still renders — falls back to primary.
func TestEventsService_List_DefaultsToPrimary(t *testing.T) {
	repo := connectedRepo()
	client := &mockCalendar{}

	if _, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.calls != 1 {
		t.Fatalf("google calls = %d, want 1", client.calls)
	}
}

// The per-request fan-out is bounded even if a stored row exceeds the cap.
func TestEventsService_List_BoundsFanOut(t *testing.T) {
	repo := connectedRepo("c1", "c2", "c3", "c4", "c5", "c6", "c7")
	client := &mockCalendar{}

	if _, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.calls != googlecal.MaxSelectedCalendars {
		t.Errorf("google calls = %d, want %d", client.calls, googlecal.MaxSelectedCalendars)
	}
}

// Events from several calendars come back in a stable order so the same data
// never renders as a reshuffled overlay.
func TestEventsService_List_SortsMergedEvents(t *testing.T) {
	repo := connectedRepo("a", "b")
	client := &mockCalendar{eventsByCalendar: map[string][]googlecal.CalendarEvent{
		"a": {{ID: "late", CalendarID: "a", Start: mustTime(t, "2026-08-03T15:00:00Z"), End: mustTime(t, "2026-08-03T16:00:00Z")}},
		"b": {{ID: "early", CalendarID: "b", Start: mustTime(t, "2026-08-03T09:00:00Z"), End: mustTime(t, "2026-08-03T10:00:00Z")}},
	}}

	resp, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Events) != 2 || resp.Events[0].ID != "early" {
		t.Errorf("events not ordered by start: %+v", resp.Events)
	}
}

// A recovered fetch clears the recorded failure, so a stale reconnect prompt
// cannot outlive the outage that caused it.
func TestEventsService_List_SuccessClearsRecordedError(t *testing.T) {
	previous := "Google Calendar could not be reached"
	repo := connectedRepo("primary")
	repo.conn.LastError = &previous

	if _, err := googlecal.NewEventsService(repo, &mockCalendar{}).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.setErrCalls != 1 || repo.lastSetErr != nil {
		t.Errorf("last_error not cleared: calls=%d value=%v", repo.setErrCalls, repo.lastSetErr)
	}
}

// The response must never carry token material in any field.
func TestEventsService_List_NeverLeaksToken(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{eventsByCalendar: map[string][]googlecal.CalendarEvent{
		"primary": {{ID: "evt-1", Title: "Standup", CalendarID: "primary", Start: mustTime(t, "2026-08-03T09:00:00Z"), End: mustTime(t, "2026-08-03T10:00:00Z")}},
	}}

	resp, err := googlecal.NewEventsService(repo, client).
		List(context.Background(), "u1", "2026-08-03", "2026-08-03", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "refresh-token") {
		t.Errorf("response carries token material: %s", encoded)
	}
}
