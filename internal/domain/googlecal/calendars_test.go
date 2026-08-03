package googlecal_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

// spyInvalidator records cache invalidations so tests can prove a selection
// change takes effect immediately rather than after the TTL.
type spyInvalidator struct {
	calls []string
}

func (s *spyInvalidator) InvalidateUser(userID string) { s.calls = append(s.calls, userID) }

func googleCalendars() []googlecal.Calendar {
	return []googlecal.Calendar{
		{ID: "primary", Summary: "Personal", BackgroundColor: "#4285f4", Primary: true},
		{ID: "team@example.com", Summary: "Team", BackgroundColor: "#0b8043"},
		{ID: "holidays@example.com", Summary: "Holidays", BackgroundColor: "#f6bf26"},
	}
}

func TestCalendarService_List_ShowsNamesAndColours(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	views, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(views) != 3 {
		t.Fatalf("calendars = %d, want 3", len(views))
	}
	if views[0].Summary != "Personal" || views[0].BackgroundColor != "#4285f4" {
		t.Errorf("names/colours not surfaced: %+v", views[0])
	}
	if !views[0].Primary {
		t.Error("primary flag lost")
	}
}

// A connection with no stored selection must show primary as selected, matching
// what the overlay actually renders.
func TestCalendarService_List_DefaultsToPrimarySelected(t *testing.T) {
	repo := connectedRepo() // no selection stored
	client := &mockCalendar{calendars: googleCalendars()}

	views, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, v := range views {
		if v.Primary && !v.Selected {
			t.Error("primary calendar not selected by default")
		}
		if !v.Primary && v.Selected {
			t.Errorf("non-primary %q selected by default", v.ID)
		}
	}
}

func TestCalendarService_List_ReflectsStoredSelection(t *testing.T) {
	repo := connectedRepo("team@example.com")
	client := &mockCalendar{calendars: googleCalendars()}

	views, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	selected := map[string]bool{}
	for _, v := range views {
		selected[v.ID] = v.Selected
	}
	if !selected["team@example.com"] {
		t.Error("stored selection not reflected")
	}
	// An explicit selection must NOT re-add primary — the default applies only
	// when nothing is stored.
	if selected["primary"] {
		t.Error("primary selected despite an explicit selection excluding it")
	}
}

// A selected calendar deleted on Google's side simply disappears; the rest
// still render.
func TestCalendarService_List_DropsStaleCalendar(t *testing.T) {
	repo := connectedRepo("primary", "deleted@example.com")
	client := &mockCalendar{calendars: googleCalendars()}

	views, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		List(context.Background(), "u1")
	if err != nil {
		t.Fatalf("stale selection caused an error: %v", err)
	}

	for _, v := range views {
		if v.ID == "deleted@example.com" {
			t.Error("deleted calendar rendered as a phantom entry")
		}
	}
	if len(views) != 3 {
		t.Errorf("calendars = %d, want the 3 that still exist", len(views))
	}
}

func TestCalendarService_UpdateSelection_Persists(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	views, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{"team@example.com", "holidays@example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := repo.conn.SelectedCalendarIDs; len(got) != 2 || got[0] != "team@example.com" {
		t.Errorf("stored selection = %v", got)
	}
	// The response reflects the new state, so the client need not refetch.
	selected := map[string]bool{}
	for _, v := range views {
		selected[v.ID] = v.Selected
	}
	if !selected["team@example.com"] || !selected["holidays@example.com"] || selected["primary"] {
		t.Errorf("returned selection wrong: %+v", selected)
	}
}

// IDs are stored, never display names — names change, IDs do not.
func TestCalendarService_UpdateSelection_StoresIDsNotNames(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	if _, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{"team@example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, stored := range repo.conn.SelectedCalendarIDs {
		if stored == "Team" {
			t.Fatal("a display name was stored instead of an ID")
		}
	}
	if repo.conn.SelectedCalendarIDs[0] != "team@example.com" {
		t.Errorf("stored = %q, want the calendar ID", repo.conn.SelectedCalendarIDs[0])
	}
}

// The cap is server-side: a disabled checkbox is a hint, not enforcement.
func TestCalendarService_UpdateSelection_EnforcesCap(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	_, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{"c1", "c2", "c3", "c4", "c5", "c6"})

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want a typed AppError", err)
	}
	if appErr.Code != apperror.ErrInvalidInput || appErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("code/status = %q/%d, want INVALID_INPUT/422", appErr.Code, appErr.Status)
	}
	// The rejected write must not have touched the stored selection.
	if len(repo.conn.SelectedCalendarIDs) != 1 || repo.conn.SelectedCalendarIDs[0] != "primary" {
		t.Errorf("selection changed despite the cap error: %v", repo.conn.SelectedCalendarIDs)
	}
}

// Exactly at the cap is allowed — an off-by-one here would block a legitimate
// selection.
func TestCalendarService_UpdateSelection_AllowsExactlyFive(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	if _, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{"c1", "c2", "c3", "c4", "c5"}); err != nil {
		t.Fatalf("five calendars rejected: %v", err)
	}
}

// Duplicates are collapsed, not rejected: double-sending the same calendar is
// not an attempt to exceed the cap.
func TestCalendarService_UpdateSelection_CollapsesDuplicates(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	if _, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{"a", "a", "a", "a", "a", "a", "b"}); err != nil {
		t.Fatalf("duplicates rejected: %v", err)
	}

	if got := repo.conn.SelectedCalendarIDs; len(got) != 2 {
		t.Errorf("stored = %v, want duplicates collapsed to 2", got)
	}
}

// Selecting nothing is a valid instruction; it must not be confused with a
// malformed request.
func TestCalendarService_UpdateSelection_AllowsEmpty(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	if _, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{}); err != nil {
		t.Fatalf("empty selection rejected: %v", err)
	}
	if len(repo.conn.SelectedCalendarIDs) != 0 {
		t.Errorf("stored = %v, want empty", repo.conn.SelectedCalendarIDs)
	}
}

func TestCalendarService_UpdateSelection_RejectsEmptyID(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}

	_, err := googlecal.NewCalendarService(repo, client, &spyInvalidator{}).
		UpdateSelection(context.Background(), "u1", []string{"a", ""})

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperror.ErrInvalidInput {
		t.Errorf("error = %v, want INVALID_INPUT", err)
	}
}

// The overlay must reflect a new selection immediately — a picker whose effect
// appears three minutes later reads as broken.
func TestCalendarService_UpdateSelection_InvalidatesCache(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}
	spy := &spyInvalidator{}

	if _, err := googlecal.NewCalendarService(repo, client, spy).
		UpdateSelection(context.Background(), "u1", []string{"team@example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(spy.calls) != 1 || spy.calls[0] != "u1" {
		t.Errorf("cache invalidations = %v, want one for u1", spy.calls)
	}
}

// A rejected selection must not invalidate the cache — nothing changed.
func TestCalendarService_UpdateSelection_NoInvalidationOnRejection(t *testing.T) {
	repo := connectedRepo("primary")
	client := &mockCalendar{calendars: googleCalendars()}
	spy := &spyInvalidator{}

	if _, err := googlecal.NewCalendarService(repo, client, spy).
		UpdateSelection(context.Background(), "u1", []string{"c1", "c2", "c3", "c4", "c5", "c6"}); err == nil {
		t.Fatal("expected a cap error")
	}

	if len(spy.calls) != 0 {
		t.Errorf("cache invalidated on a rejected write: %v", spy.calls)
	}
}

// Unlike the events endpoint, the picker surfaces Google failures — showing an
// empty list would read as "you have no calendars".
func TestCalendarService_List_SurfacesGoogleFailure(t *testing.T) {
	tests := []struct {
		name       string
		repo       *eventsRepo
		client     *mockCalendar
		wantCode   string
		wantStatus int
		wantDelete int
	}{
		{
			name:       "transient failure surfaces as a bad gateway",
			repo:       connectedRepo("primary"),
			client:     &mockCalendar{err: googlecal.ErrCalendarUnavailable},
			wantCode:   apperror.ErrGoogleAuthFailed,
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "invalid_grant disconnects and asks for reconnect",
			repo:       connectedRepo("primary"),
			client:     &mockCalendar{err: googlecal.ErrCalendarUnauthorized},
			wantCode:   apperror.ErrGoogleNotConnected,
			wantStatus: http.StatusConflict,
			wantDelete: 1,
		},
		{
			name:       "unconfigured integration is a typed 503",
			repo:       connectedRepo("primary"),
			client:     &mockCalendar{disabled: true},
			wantCode:   apperror.ErrGoogleAuthFailed,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name: "never connected",
			repo: &eventsRepo{getErr: apperror.New(
				http.StatusConflict, apperror.ErrGoogleNotConnected, "not connected")},
			client:     &mockCalendar{},
			wantCode:   apperror.ErrGoogleNotConnected,
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := googlecal.NewCalendarService(tt.repo, tt.client, &spyInvalidator{}).
				List(context.Background(), "u1")

			var appErr *apperror.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("error = %v, want a typed AppError", err)
			}
			if appErr.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", appErr.Code, tt.wantCode)
			}
			if appErr.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", appErr.Status, tt.wantStatus)
			}
			if tt.repo.deleteCalls != tt.wantDelete {
				t.Errorf("delete calls = %d, want %d", tt.repo.deleteCalls, tt.wantDelete)
			}
		})
	}
}
