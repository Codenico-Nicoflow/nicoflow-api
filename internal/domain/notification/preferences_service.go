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
	if err := validateLeadTime("beforeDueMinutes", u.BeforeDueMinutes); err != nil {
		return PreferencesView{}, err
	}
	if err := validateLeadTime("afterDueMinutes", u.AfterDueMinutes); err != nil {
		return PreferencesView{}, err
	}
	p, err := s.repo.UpsertPreferences(ctx, userID, u)
	if err != nil {
		return PreferencesView{}, err
	}
	return preferencesToView(p), nil
}

// validateLeadTime enforces 0 ≤ minutes ≤ LeadTimeMax when the field is present.
func validateLeadTime(field string, minutes *int) error {
	if minutes == nil {
		return nil
	}
	if *minutes < 0 || *minutes > LeadTimeMax {
		return apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput,
			field+" must be between 0 and "+strconv.Itoa(LeadTimeMax)+" minutes")
	}
	return nil
}
