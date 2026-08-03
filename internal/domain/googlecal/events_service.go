package googlecal

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// EventsService reads the user's Google events for a date range.
//
// Its defining property is that it does not fail. Every Google-side problem is
// converted into a status the client can render, because this overlay is context
// on someone's task calendar — a failure here must never be able to take down
// the view that shows the user their own work.
type EventsService interface {
	// List returns the events overlapping [from, to] in the user's timezone.
	// It returns an error ONLY for caller mistakes (a malformed or oversized
	// range). Google failures come back as a status with an empty list.
	List(ctx context.Context, userID, from, to string, refresh bool) (EventsResponse, error)
}

type eventsService struct {
	repo   Repository
	client CalendarClient
	cache  *eventCache
}

// NewEventsService wires the events reader.
func NewEventsService(repo Repository, client CalendarClient) EventsService {
	return &eventsService{repo: repo, client: client, cache: newEventCache()}
}

// List fetches the overlay for a date range.
//
// The whole method is a funnel from "many ways this can go wrong" down to two
// outcomes: events with a status, or a 422 the caller caused. Read the returns:
// only parseEventRange can produce a non-nil error.
func (s *eventsService) List(ctx context.Context, userID, from, to string, refresh bool) (EventsResponse, error) {
	window, err := parseEventRange(from, to)
	if err != nil {
		return EventsResponse{}, err
	}

	// An unconfigured integration is not a failure to report — nobody can be
	// connected, so "disconnected" is the honest answer and the UI already
	// renders it as "connect your calendar".
	if !s.client.Enabled() {
		return emptyResponse(StatusDisconnected), nil
	}

	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		return emptyResponse(s.statusForConnectionError(ctx, userID, err)), nil
	}

	loc := s.location(ctx, userID)
	// The window is a pair of local dates; Google needs instants. Resolving the
	// bounds in the user's zone is what makes "the 5th" mean their 5th, and it
	// is where a UTC shortcut would silently drop the first or last hours of the
	// range for anyone not on UTC.
	fromAt := time.Date(window.from.Year(), window.from.Month(), window.from.Day(), 0, 0, 0, 0, loc)
	// Exclusive upper bound: `to` is an inclusive date, so the instant is
	// midnight at the START of the following day.
	toAt := time.Date(window.to.Year(), window.to.Month(), window.to.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)

	calendarIDs := selectedOrDefault(conn.SelectedCalendarIDs)
	key := cacheKey(userID, calendarIDs, fromAt, toAt)
	if !refresh {
		if cached, ok := s.cache.get(key); ok {
			return response(cached, loc), nil
		}
	}

	events, err := s.fetchAll(ctx, conn.RefreshToken, calendarIDs, fromAt, toAt)
	if err != nil {
		return emptyResponse(s.statusForFetchError(ctx, userID, err)), nil
	}

	s.cache.put(key, events)
	// A successful fetch resolves any previously recorded failure. Left alone,
	// the reconnect prompt from last week's outage would outlive the outage.
	if conn.LastError != nil {
		if err := s.repo.SetError(ctx, userID, nil); err != nil {
			log.Warn().Err(err).Str("user_id", userID).Msg("googlecal: could not clear last_error")
		}
	}

	return response(events, loc), nil
}

// fetchAll queries each selected calendar and merges the results.
//
// Sequential rather than concurrent: the fan-out is capped at
// MaxSelectedCalendars, and running five goroutines to save a few hundred
// milliseconds would add cancellation and partial-failure handling to the one
// code path that most needs to stay simple enough to reason about.
//
// A calendar that has been deleted or unshared is dropped and the rest still
// return — one stale entry in a picker must not blank the whole overlay (AC7).
func (s *eventsService) fetchAll(ctx context.Context, token Secret, calendarIDs []string, from, to time.Time) ([]CalendarEvent, error) {
	var all []CalendarEvent

	for _, id := range calendarIDs {
		events, err := s.client.ListEvents(ctx, token, id, from, to)
		if errors.Is(err, ErrCalendarNotFound) {
			log.Info().Str("calendar_id", id).Msg("googlecal: skipping unavailable calendar")
			continue
		}
		if err != nil {
			// Anything else is connection-wide — a dead token or an outage will
			// hit every remaining calendar identically, so failing now avoids
			// four more doomed round-trips.
			return nil, err
		}
		all = append(all, events...)
	}

	// Stable order so the client renders the same overlay for the same data and
	// the cache cannot introduce a visual reshuffle. ID breaks ties to keep the
	// order total.
	sort.Slice(all, func(i, j int) bool {
		if !all[i].Start.Equal(all[j].Start) {
			return all[i].Start.Before(all[j].Start)
		}
		return all[i].ID < all[j].ID
	})

	return all, nil
}

// statusForConnectionError maps a failure to load the stored connection.
func (s *eventsService) statusForConnectionError(ctx context.Context, userID string, err error) GoogleStatus {
	var appErr *apperror.AppError
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case apperror.ErrGoogleNotConnected:
			return StatusDisconnected
		case apperror.ErrGoogleAuthFailed:
			// The stored token cannot be decrypted — unrecoverable, and
			// indistinguishable from a revoked grant from the user's side.
			return StatusDisconnected
		}
	}
	log.Error().Err(err).Str("user_id", userID).Msg("googlecal: could not load connection")
	return StatusError
}

// statusForFetchError maps a Google failure and applies the terminal case.
//
// invalid_grant is the one error that must change stored state: the grant is
// dead, so the connection is deleted (which clears the token) and the user is
// told to reconnect. Anything else leaves the connection intact — deleting on a
// transient 503 would log users out of an integration that was about to recover.
func (s *eventsService) statusForFetchError(ctx context.Context, userID string, err error) GoogleStatus {
	if errors.Is(err, ErrCalendarUnauthorized) {
		log.Warn().Str("user_id", userID).Msg("googlecal: grant rejected; disconnecting")
		s.cache.invalidateUser(userID)
		// Delete rather than mark: a token Google has rejected is worthless, and
		// keeping the ciphertext around is a stored secret with no purpose. No
		// retry and no backoff — invalid_grant never becomes valid again.
		if delErr := s.repo.Delete(ctx, userID); delErr != nil {
			log.Error().Err(delErr).Str("user_id", userID).Msg("googlecal: could not delete rejected connection")
		}
		return StatusDisconnected
	}

	log.Warn().Err(err).Str("user_id", userID).Msg("googlecal: event fetch failed")
	// Recorded so Settings can explain the degraded state instead of silently
	// showing nothing. The message is ours, never Google's body, which can echo
	// request parameters.
	msg := "Google Calendar could not be reached"
	if setErr := s.repo.SetError(ctx, userID, &msg); setErr != nil {
		log.Warn().Err(setErr).Str("user_id", userID).Msg("googlecal: could not record last_error")
	}
	return StatusError
}

// location resolves the user's timezone, falling back to UTC.
//
// A bad zone degrades to UTC rather than erroring: an unparseable IANA name is a
// data problem that must not cost the user their calendar.
func (s *eventsService) location(ctx context.Context, userID string) *time.Location {
	tz, err := s.repo.UserTimezone(ctx, userID)
	if err != nil {
		log.Warn().Err(err).Str("user_id", userID).Msg("googlecal: could not read timezone; using UTC")
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		log.Warn().Str("user_id", userID).Str("timezone", tz).Msg("googlecal: unknown timezone; using UTC")
		return time.UTC
	}
	return loc
}

// InvalidateUser drops a user's cached ranges. Called when the selection changes
// (NIC-1857) so the next read reflects the new set immediately — a picker whose
// effect appears three minutes later reads as a broken picker.
func (s *eventsService) InvalidateUser(userID string) { s.cache.invalidateUser(userID) }

// eventRange is a validated pair of local dates.
type eventRange struct{ from, to time.Time }

// parseEventRange validates both bounds and the span. Both are required — an
// unbounded query would fan out across every selected calendar with no ceiling.
func parseEventRange(from, to string) (eventRange, error) {
	if from == "" || to == "" {
		return eventRange{}, invalidRange("from and to are both required")
	}
	fromDay, err := time.Parse(eventDateLayout, from)
	if err != nil {
		return eventRange{}, invalidRange("from must be an ISO date (YYYY-MM-DD)")
	}
	toDay, err := time.Parse(eventDateLayout, to)
	if err != nil {
		return eventRange{}, invalidRange("to must be an ISO date (YYYY-MM-DD)")
	}
	if toDay.Before(fromDay) {
		return eventRange{}, invalidRange("to must not be before from")
	}
	// Inclusive span: from==to is one day, not zero.
	if int(toDay.Sub(fromDay).Hours()/24)+1 > maxEventRangeSpanDays {
		return eventRange{}, invalidRange("range must not span more than 62 days")
	}
	return eventRange{from: fromDay, to: toDay}, nil
}

func invalidRange(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, msg)
}

// selectedOrDefault falls back to the primary calendar when nothing is selected,
// so a connection made before the picker existed still renders something.
func selectedOrDefault(ids []string) []string {
	if len(ids) == 0 {
		return []string{"primary"}
	}
	if len(ids) > MaxSelectedCalendars {
		// Defence in depth: the cap is enforced on write, but a row that predates
		// it or was edited directly must not turn into an unbounded fan-out.
		return ids[:MaxSelectedCalendars]
	}
	return ids
}

func response(events []CalendarEvent, loc *time.Location) EventsResponse {
	// Non-nil so the field marshals as [] — the client maps over it directly.
	views := make([]GoogleEventView, 0, len(events))
	for _, e := range events {
		views = append(views, e.View(loc))
	}
	return EventsResponse{Events: views, Status: StatusOK}
}

func emptyResponse(status GoogleStatus) EventsResponse {
	return EventsResponse{Events: []GoogleEventView{}, Status: status}
}
