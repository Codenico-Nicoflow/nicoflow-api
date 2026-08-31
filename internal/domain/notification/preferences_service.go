package notification

import (
	"context"
	"net/http"
	"strconv"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

func (s *service) GetPreferences(ctx context.Context, userID string) (PreferencesView, error) {
	p, err := s.repo.GetPreferences(ctx, userID)
	if err != nil {
		return PreferencesView{}, err
	}
	return preferencesToView(p), nil
}

func (s *service) UpdatePreferences(ctx context.Context, userID string, u UpdatePreferences) (PreferencesView, error) {
	if err := validateHour("morningHour", u.MorningHour, MorningHourMin, MorningHourMax); err != nil {
		return PreferencesView{}, err
	}
	if err := validateHour("eveningHour", u.EveningHour, EveningHourMin, EveningHourMax); err != nil {
		return PreferencesView{}, err
	}
	p, err := s.repo.UpsertPreferences(ctx, userID, u)
	if err != nil {
		return PreferencesView{}, err
	}
	return preferencesToView(p), nil
}

// validateHour enforces min ≤ hour ≤ max when the field is present. Mirrors the
// migration-035 CHECK so a bad value is rejected before the DB round-trip.
func validateHour(field string, hour *int, min, max int) error {
	if hour == nil {
		return nil
	}
	if *hour < min || *hour > max {
		return apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput,
			field+" must be between "+strconv.Itoa(min)+" and "+strconv.Itoa(max))
	}
	return nil
}
