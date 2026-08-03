package googlecal

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// CalendarService lists the user's Google calendars and persists which of them
// overlay the Nicoflow calendar (NIC-1857).
//
// A Google account is not one calendar. Importing everything would tint every
// day and invert the signal from "you are booked" into "ignore this colour" —
// birthday and holiday calendars alone would flood the all-day rail.
type CalendarService interface {
	// List returns the user's calendars with their current selected state.
	List(ctx context.Context, userID string) ([]CalendarView, error)
	// UpdateSelection replaces the selection, enforcing the cap.
	UpdateSelection(ctx context.Context, userID string, calendarIDs []string) ([]CalendarView, error)
}

// EventCacheInvalidator lets the selection service drop cached ranges belonging
// to a user. Defined here, in the consumer, so this service depends on the
// capability rather than on the events service as a whole.
type EventCacheInvalidator interface {
	InvalidateUser(userID string)
}

type calendarService struct {
	repo   Repository
	client CalendarClient
	cache  EventCacheInvalidator
}

// NewCalendarService wires the calendar picker service.
func NewCalendarService(repo Repository, client CalendarClient, cache EventCacheInvalidator) CalendarService {
	return &calendarService{repo: repo, client: client, cache: cache}
}

// List returns the user's calendars, each marked with whether it is selected.
//
// Selection state is merged in here rather than left to the client: the client
// would otherwise have to intersect two lists and would get the stale-calendar
// case wrong — a selected ID that no longer exists in Google's list must simply
// disappear, not render as a phantom checkbox.
func (s *calendarService) List(ctx context.Context, userID string) ([]CalendarView, error) {
	if !s.client.Enabled() {
		return nil, unavailable()
	}

	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	calendars, err := s.client.ListCalendars(ctx, conn.RefreshToken)
	if err != nil {
		return nil, s.classify(ctx, userID, err)
	}

	selected := conn.SelectedCalendarIDs
	// A connection predating the picker has no selection; the overlay defaults
	// to primary, so the picker must show that same default rather than nothing.
	defaulted := len(selected) == 0

	views := make([]CalendarView, 0, len(calendars))
	for _, c := range calendars {
		views = append(views, CalendarView{
			ID:              c.ID,
			Summary:         c.Summary,
			BackgroundColor: c.BackgroundColor,
			Primary:         c.Primary,
			Selected:        slices.Contains(selected, c.ID) || (defaulted && c.Primary),
		})
	}

	return views, nil
}

// UpdateSelection replaces the stored selection.
//
// The cap is enforced HERE, not in the handler and not in the UI: a disabled
// checkbox is a hint, never enforcement, and the fan-out it bounds is a real
// per-request cost paid on the server.
func (s *calendarService) UpdateSelection(ctx context.Context, userID string, calendarIDs []string) ([]CalendarView, error) {
	cleaned, err := normaliseSelection(calendarIDs)
	if err != nil {
		return nil, err
	}

	if _, err := s.repo.UpdateSelectedCalendars(ctx, userID, cleaned); err != nil {
		return nil, err
	}

	// Without this the picker looks broken: the new selection would not affect
	// the overlay until the cached range expired minutes later.
	if s.cache != nil {
		s.cache.InvalidateUser(userID)
	}

	return s.List(ctx, userID)
}

// classify converts a Google failure into a typed error for the picker.
//
// Unlike the events endpoint, the picker DOES surface an error — it is an
// explicit user action, and silently showing an empty list would read as "you
// have no calendars" rather than "we could not reach Google".
func (s *calendarService) classify(ctx context.Context, userID string, err error) error {
	if errors.Is(err, ErrCalendarUnauthorized) {
		log.Warn().Str("user_id", userID).Msg("googlecal: grant rejected while listing calendars; disconnecting")
		if delErr := s.repo.Delete(ctx, userID); delErr != nil {
			log.Error().Err(delErr).Str("user_id", userID).Msg("googlecal: could not delete rejected connection")
		}
		if s.cache != nil {
			s.cache.InvalidateUser(userID)
		}
		return apperror.New(http.StatusConflict, apperror.ErrGoogleNotConnected,
			"the Google connection is no longer valid; reconnect required")
	}

	log.Warn().Err(err).Str("user_id", userID).Msg("googlecal: could not list calendars")
	return apperror.New(http.StatusBadGateway, apperror.ErrGoogleAuthFailed,
		"could not reach Google Calendar")
}

// normaliseSelection validates, de-duplicates and caps a requested selection.
//
// De-duplication comes before the cap check on purpose: sending the same
// calendar six times is not an attempt to exceed the cap, and rejecting it would
// be a confusing error for a client that merely double-sent.
func normaliseSelection(ids []string) ([]string, error) {
	if ids == nil {
		return nil, invalidSelection("calendarIds is required")
	}

	cleaned := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return nil, invalidSelection("calendarIds must not contain empty values")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}

	if len(cleaned) > MaxSelectedCalendars {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"at most 5 calendars may be selected")
	}

	return cleaned, nil
}

func invalidSelection(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, msg)
}
