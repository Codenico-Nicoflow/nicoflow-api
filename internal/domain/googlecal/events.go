package googlecal

import (
	"context"
	"errors"
	"time"
)

// GoogleStatus tells the client whether the overlay it is rendering is complete.
//
// This exists because the alternative — an empty event list — is ambiguous: a
// user with no meetings and a user whose token just died look identical, and the
// second one must not be shown a clean calendar. Every events response carries a
// status so "we could not fetch" is never mistaken for "you are free".
type GoogleStatus string

const (
	// StatusOK means the fetch succeeded and the list is complete.
	StatusOK GoogleStatus = "ok"
	// StatusDisconnected means there is no usable connection: never connected,
	// or the grant is dead and the stored token has been cleared. The user must
	// reconnect — retrying will not fix it.
	StatusDisconnected GoogleStatus = "disconnected"
	// StatusError means the connection is fine but Google could not be reached
	// or refused this request. Transient; retrying later may succeed.
	StatusError GoogleStatus = "error"
)

// maxEventRangeSpanDays bounds a single events query, matching the task calendar
// endpoint's cap (E-051). The two ranges are rendered on the same grid, so a
// wider window here would fetch events for days the client cannot draw — and the
// per-calendar fan-out means the cost is paid up to MaxSelectedCalendars times.
const maxEventRangeSpanDays = 62

// eventDateLayout is the wire format for an all-day event's date (YYYY-MM-DD).
const eventDateLayout = "2006-01-02"

// Sentinel errors from the calendar client, defined here in the consumer package
// so the service can classify a failure without importing the implementation.
var (
	// ErrCalendarUnauthorized is Google's invalid_grant: the refresh token is
	// dead. Distinguished from every other failure because it is TERMINAL —
	// the user revoked access, changed their password, or the grant expired.
	// Retrying is guaranteed to fail, so the service disconnects rather than
	// backing off.
	ErrCalendarUnauthorized = errors.New("googlecal: google refused the credentials")
	// ErrCalendarUnavailable covers every transient failure — network, 5xx,
	// quota. The connection survives; the fetch does not.
	ErrCalendarUnavailable = errors.New("googlecal: google calendar is unavailable")
	// ErrCalendarNotFound means the calendar is gone or no longer shared. Per
	// calendar, not per connection: one dead calendar must not fail the fetch.
	ErrCalendarNotFound = errors.New("googlecal: calendar not found")
)

// CalendarEvent is one occurrence as the API layer returns it, before mapping.
// Recurring series are already expanded by the client (singleEvents=true), so
// this is always a concrete occurrence and never carries an RRULE.
type CalendarEvent struct {
	ID         string
	Title      string
	CalendarID string
	HTMLLink   string
	// AllDay events carry dates, not instants — see Start/End.
	AllDay bool
	// Start and End are absolute instants for timed events. For all-day events
	// they are midnight-to-midnight in the user's zone, which is what lets the
	// client place them on a day without re-deriving the boundary.
	Start time.Time
	// End is exclusive, matching Google's own semantics.
	End time.Time
}

// Calendar is one entry from the user's calendar list (NIC-1857).
type Calendar struct {
	ID              string
	Summary         string
	BackgroundColor string
	Primary         bool
}

// CalendarClient reads from Google Calendar. Implemented by internal/google and
// mocked in tests so CI never calls out.
//
// It takes a refresh token rather than an access token: access tokens live an
// hour and would otherwise have to be cached, refreshed and invalidated by every
// caller. Exchanging one per request is a single extra call that keeps token
// lifecycle entirely inside the implementation.
type CalendarClient interface {
	Enabled() bool
	// ListEvents fetches one calendar's occurrences in [from, to). Returns
	// ErrCalendarNotFound for a deleted or unshared calendar so the caller can
	// drop it without failing the whole fetch.
	ListEvents(ctx context.Context, refreshToken Secret, calendarID string, from, to time.Time) ([]CalendarEvent, error)
	// ListCalendars returns the calendars the user can read (NIC-1857).
	ListCalendars(ctx context.Context, refreshToken Secret) ([]Calendar, error)
}

// GoogleEventView is the wire shape (SPEC §3). IDs are strings, and there is no
// token field in any form.
type GoogleEventView struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Start      string `json:"start"`
	End        string `json:"end"`
	AllDay     bool   `json:"allDay"`
	CalendarID string `json:"calendarId"`
	HTMLLink   string `json:"htmlLink"`
}

// EventsResponse is what GET /v1/calendar/google-events returns.
//
// Events and Status travel together on purpose: a client that reads the list
// without the status cannot tell a free day from a failed fetch.
type EventsResponse struct {
	Events []GoogleEventView `json:"events"`
	Status GoogleStatus      `json:"googleStatus"`
}

// View projects an event for the wire in the user's timezone.
//
// Times are RFC3339 with the user's offset rather than UTC: the client places
// events on an hour grid, and shipping UTC would make every consumer re-derive
// the local hour — the exact conversion that goes wrong across a DST boundary.
// Doing it once here means the browser, the mobile app and any test all agree.
//
// All-day events are emitted as plain dates. They have no instant: a birthday is
// the 4th everywhere, and giving it a time would make it drift across the day
// boundary for anyone in another zone.
func (e CalendarEvent) View(loc *time.Location) GoogleEventView {
	start, end := e.Start.In(loc).Format(time.RFC3339), e.End.In(loc).Format(time.RFC3339)
	if e.AllDay {
		start, end = e.Start.In(loc).Format(eventDateLayout), e.End.In(loc).Format(eventDateLayout)
	}

	return GoogleEventView{
		ID:         e.ID,
		Title:      e.Title,
		Start:      start,
		End:        end,
		AllDay:     e.AllDay,
		CalendarID: e.CalendarID,
		HTMLLink:   e.HTMLLink,
	}
}

// CalendarView is one calendar in the picker (NIC-1857).
type CalendarView struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	BackgroundColor string `json:"backgroundColor"`
	Primary         bool   `json:"primary"`
	Selected        bool   `json:"selected"`
}
