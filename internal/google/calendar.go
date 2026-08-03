package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

const (
	calendarAPIBase = "https://www.googleapis.com/calendar/v3"

	// eventPageSize is how many events one page returns. Google's own maximum is
	// 2500; 250 is its default. A month of a busy calendar fits well inside one
	// page at this size, so the pagination loop is a correctness guarantee for
	// outliers rather than the normal path.
	eventPageSize = 250

	// maxEventPages bounds the pagination loop. A calendar with more than 2500
	// events inside a 62-day window is pathological, and following nextPageToken
	// without a ceiling turns one request into an unbounded sequence of calls.
	maxEventPages = 10
)

// ListEvents fetches one calendar's occurrences in [from, to).
//
// singleEvents=true makes Google expand recurring series into concrete
// instances. That is deliberate: the alternative is receiving RRULE strings and
// re-implementing RFC 5545 expansion — including EXDATE, moved instances and
// DST-crossing rules — which is a well-known source of "the meeting shows an
// hour off twice a year" bugs.
func (c *Client) ListEvents(ctx context.Context, refreshToken googlecal.Secret, calendarID string, from, to time.Time) ([]googlecal.CalendarEvent, error) {
	if !c.enabled {
		return nil, googlecal.ErrOAuthDisabled
	}

	accessToken, err := c.accessToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	var events []googlecal.CalendarEvent
	pageToken := ""

	for fetched := 0; fetched < maxEventPages; fetched++ {
		q := url.Values{
			"singleEvents": {"true"},
			// Ordering by start time is only permitted with singleEvents=true and
			// keeps the merged result close to sorted before the service's own sort.
			"orderBy":    {"startTime"},
			"timeMin":    {from.Format(time.RFC3339)},
			"timeMax":    {to.Format(time.RFC3339)},
			"maxResults": {strconv.Itoa(eventPageSize)},
			// Cancelled instances of a recurring series would otherwise appear as
			// events the user has already declined or deleted.
			"showDeleted": {"false"},
		}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}

		endpoint := calendarAPIBase + "/calendars/" + url.PathEscape(calendarID) + "/events?" + q.Encode()

		page, err := getJSON[eventListResponse](ctx, c, endpoint, accessToken)
		if err != nil {
			return nil, err
		}

		for _, item := range page.Items {
			event, ok := item.toDomain(calendarID)
			if !ok {
				// An event with no usable start — Google returns these for some
				// cancelled or malformed entries. Skipped rather than failed:
				// one bad row must not cost the user the whole overlay.
				continue
			}
			events = append(events, event)
		}

		if page.NextPageToken == "" {
			return events, nil
		}
		pageToken = page.NextPageToken
	}

	// Hit the page ceiling: return what was collected rather than erroring. A
	// partial overlay is more useful than none, and the cap is high enough that
	// reaching it means the calendar is unusual, not that the fetch is broken.
	return events, nil
}

// ListCalendars returns the calendars the user can read (NIC-1857).
func (c *Client) ListCalendars(ctx context.Context, refreshToken googlecal.Secret) ([]googlecal.Calendar, error) {
	if !c.enabled {
		return nil, googlecal.ErrOAuthDisabled
	}

	accessToken, err := c.accessToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	list, err := getJSON[calendarListResponse](ctx, c, calendarAPIBase+"/users/me/calendarList?minAccessRole=reader", accessToken)
	if err != nil {
		return nil, err
	}

	calendars := make([]googlecal.Calendar, 0, len(list.Items))
	for _, item := range list.Items {
		calendars = append(calendars, googlecal.Calendar{
			ID:              item.ID,
			Summary:         item.Summary,
			BackgroundColor: item.BackgroundColor,
			Primary:         item.Primary,
		})
	}
	return calendars, nil
}

// accessToken exchanges the stored refresh token for a short-lived access token.
//
// Done per request rather than cached. An access token lives an hour, so caching
// one would mean holding a second live credential in memory and inventing
// invalidation for it — for a saving of one HTTP call on a path that already
// caches its actual result upstream.
func (c *Client) accessToken(ctx context.Context, refreshToken googlecal.Secret) (string, error) {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {refreshToken.Reveal()},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codeExchangeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("google: refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", googlecal.ErrCalendarUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		// invalid_grant is the terminal case: the user revoked access, changed
		// their password, or the token expired from disuse. It is reported
		// distinctly because it is the ONLY failure that must not be retried.
		if isInvalidGrant(resp.StatusCode, body) {
			return "", googlecal.ErrCalendarUnauthorized
		}
		return "", fmt.Errorf("%w: token refresh status %d", googlecal.ErrCalendarUnavailable, resp.StatusCode)
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("%w: decode refresh response: %v", googlecal.ErrCalendarUnavailable, err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("%w: refresh returned no access token", googlecal.ErrCalendarUnavailable)
	}
	return token.AccessToken, nil
}

// getJSON performs an authorized GET and decodes the body.
//
// Status mapping is the point of this function: 401/403-with-invalid-credentials
// is terminal, 404/410 is one dead calendar, and everything else is transient.
// The caller distinguishes "reconnect" from "retry later" entirely from these.
func getJSON[T calendarResponse](ctx context.Context, c *Client, endpoint, accessToken string) (T, error) {
	var out T

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return out, fmt.Errorf("google: calendar request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("%w: %v", googlecal.ErrCalendarUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to decode
	case http.StatusUnauthorized:
		return out, googlecal.ErrCalendarUnauthorized
	case http.StatusNotFound, http.StatusGone:
		// The calendar was deleted or unshared. Per-calendar, so the service can
		// drop it and keep the others.
		return out, googlecal.ErrCalendarNotFound
	default:
		// Includes 403 (quota/rate limit) and every 5xx. All transient from the
		// caller's perspective: the connection is fine, this request is not.
		// The body is discarded rather than surfaced — Google's error payloads
		// echo request parameters.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, errorBodyLimit))
		return out, fmt.Errorf("%w: calendar status %d", googlecal.ErrCalendarUnavailable, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("%w: decode calendar response: %v", googlecal.ErrCalendarUnavailable, err)
	}
	return out, nil
}

// --- wire shapes -------------------------------------------------------------

// errorBodyLimit bounds how much of an error body is read before classifying it,
// so a misbehaving endpoint cannot stream unbounded data into memory.
const errorBodyLimit = 4 << 10

// calendarResponse constrains getJSON to the payloads this package decodes,
// which keeps the helper generic without reaching for `any`.
type calendarResponse interface {
	eventListResponse | calendarListResponse
}

type eventListResponse struct {
	Items         []eventItem `json:"items"`
	NextPageToken string      `json:"nextPageToken"`
}

type calendarListResponse struct {
	Items []calendarItem `json:"items"`
}

type calendarItem struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	BackgroundColor string `json:"backgroundColor"`
	Primary         bool   `json:"primary"`
}

// eventDateTime is Google's start/end shape. Exactly one of the two is set:
// DateTime for a timed event, Date for an all-day one. That distinction is the
// whole reason all-day events need separate handling downstream.
type eventDateTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
	TimeZone string `json:"timeZone"`
}

type eventItem struct {
	ID          string        `json:"id"`
	Summary     string        `json:"summary"`
	HTMLLink    string        `json:"htmlLink"`
	Status      string        `json:"status"`
	Location    string        `json:"location"`
	Description string        `json:"description"`
	Organizer   eventPerson   `json:"organizer"`
	Attendees   []eventPerson `json:"attendees"`
	Start       eventDateTime `json:"start"`
	End         eventDateTime `json:"end"`
}

// eventPerson covers both the organizer and each attendee — Google uses the same
// shape for both.
type eventPerson struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	Self           bool   `json:"self"`
	ResponseStatus string `json:"responseStatus"`
}

// label prefers the display name and falls back to the email, which is all
// Google sends for someone outside the user's directory.
func (p eventPerson) label() string {
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Email
}

// selfResponse finds the viewer's own RSVP.
//
// Google marks the viewer's own entry with self=true rather than making the
// caller match on email, which matters because the calendar may be shared and
// the viewer's address may differ from the account address (aliases, groups).
func (e eventItem) selfResponse() googlecal.ResponseStatus {
	for _, attendee := range e.Attendees {
		if attendee.Self {
			return googlecal.ResponseStatus(attendee.ResponseStatus)
		}
	}
	return googlecal.ResponseNone
}

// toDomain converts one wire event, reporting false when it carries no usable
// start — the caller skips those rather than failing the fetch.
//
// RFC3339 parsing preserves the offset Google sends, so an event created in
// another zone keeps its true instant and converts correctly for the viewer.
// This is what makes the DST case work: the offset comes from Google per
// occurrence, and is never inferred from the calendar's current one.
func (e eventItem) toDomain(calendarID string) (googlecal.CalendarEvent, bool) {
	if e.Status == "cancelled" {
		return googlecal.CalendarEvent{}, false
	}

	event := googlecal.CalendarEvent{
		ID:            e.ID,
		Title:         e.Summary,
		CalendarID:    calendarID,
		HTMLLink:      e.HTMLLink,
		Location:      e.Location,
		Description:   plainText(e.Description),
		Organizer:     e.Organizer.label(),
		AttendeeCount: len(e.Attendees),
		Response:      e.selfResponse(),
	}
	// Google omits the title of a private event on a shared calendar. A blank
	// block reads as a rendering bug, so it gets an honest label instead.
	if event.Title == "" {
		event.Title = "(no title)"
	}

	switch {
	case e.Start.DateTime != "":
		start, err := time.Parse(time.RFC3339, e.Start.DateTime)
		if err != nil {
			return googlecal.CalendarEvent{}, false
		}
		end, err := time.Parse(time.RFC3339, e.End.DateTime)
		if err != nil {
			// A start without a parseable end still places on the grid; Google
			// treats such events as instantaneous.
			end = start
		}
		event.Start, event.End = start, end

	case e.Start.Date != "":
		// All-day events carry a floating date with no zone. Parsed as UTC
		// midnight and marked AllDay so the view layer formats it back as a
		// plain date — attaching a real zone here would make a birthday shift a
		// day for anyone east or west of it.
		start, err := time.Parse(eventDateLayout, e.Start.Date)
		if err != nil {
			return googlecal.CalendarEvent{}, false
		}
		end, err := time.Parse(eventDateLayout, e.End.Date)
		if err != nil {
			end = start.AddDate(0, 0, 1)
		}
		event.AllDay, event.Start, event.End = true, start, end

	default:
		return googlecal.CalendarEvent{}, false
	}

	return event, true
}

// eventDateLayout is Google's all-day date format.
const eventDateLayout = "2006-01-02"

// isInvalidGrant reports whether a token-endpoint failure is the terminal
// invalid_grant case. Matched on the error field rather than the status alone,
// since Google returns 400 for several distinct token errors.
func isInvalidGrant(status int, body []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnauthorized {
		return false
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Unparseable body on a 400/401 from the token endpoint: treat as
		// terminal. A refresh token that yields an unreadable rejection is not
		// one to keep retrying with.
		return true
	}
	return payload.Error == "invalid_grant"
}
