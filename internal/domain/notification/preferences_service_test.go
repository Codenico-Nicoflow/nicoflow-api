package notification_test

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func ptrBool(b bool) *bool { return &b }
func ptrInt(i int) *int    { return &i }

func TestService_GetPreferences(t *testing.T) {
	want := notification.Preferences{
		UserID: "u1", EmailDigest: true, BeforeDueMinutes: 1440,
	}
	svc := notification.NewService(&mockRepo{
		getPreferences: func(_ context.Context, _ string) (notification.Preferences, error) {
			return want, nil
		},
	}, nil)

	got, err := svc.GetPreferences(context.Background(), "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.EmailDigest || got.BeforeDueMinutes != 1440 {
		t.Fatalf("view = %+v, want emailDigest=true beforeDueMinutes=1440", got)
	}
}

func TestService_UpdatePreferences_Validation(t *testing.T) {
	tests := []struct {
		name    string
		update  notification.UpdatePreferences
		wantErr bool
	}{
		{"before negative", notification.UpdatePreferences{BeforeDueMinutes: ptrInt(-5)}, true},
		{"before over cap", notification.UpdatePreferences{BeforeDueMinutes: ptrInt(notification.LeadTimeMax + 1)}, true},
		{"after negative", notification.UpdatePreferences{AfterDueMinutes: ptrInt(-1)}, true},
		{"after over cap", notification.UpdatePreferences{AfterDueMinutes: ptrInt(notification.LeadTimeMax + 1)}, true},
		{"before zero ok", notification.UpdatePreferences{BeforeDueMinutes: ptrInt(0)}, false},
		{"before at cap ok", notification.UpdatePreferences{BeforeDueMinutes: ptrInt(notification.LeadTimeMax)}, false},
		{"nil lead times ok", notification.UpdatePreferences{EmailDigest: ptrBool(false)}, false},

		// Reminder hours: morning 5–11, evening 18–22.
		{"morning below range", notification.UpdatePreferences{MorningHour: ptrInt(notification.MorningHourMin - 1)}, true},
		{"morning above range", notification.UpdatePreferences{MorningHour: ptrInt(notification.MorningHourMax + 1)}, true},
		{"morning at min ok", notification.UpdatePreferences{MorningHour: ptrInt(notification.MorningHourMin)}, false},
		{"morning at max ok", notification.UpdatePreferences{MorningHour: ptrInt(notification.MorningHourMax)}, false},
		{"evening below range", notification.UpdatePreferences{EveningHour: ptrInt(notification.EveningHourMin - 1)}, true},
		{"evening above range", notification.UpdatePreferences{EveningHour: ptrInt(notification.EveningHourMax + 1)}, true},
		{"evening at min ok", notification.UpdatePreferences{EveningHour: ptrInt(notification.EveningHourMin)}, false},
		{"evening at max ok", notification.UpdatePreferences{EveningHour: ptrInt(notification.EveningHourMax)}, false},
		{"nil hours ok", notification.UpdatePreferences{PushEnabled: ptrBool(true)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := notification.NewService(&mockRepo{
				upsertPrefs: func(_ context.Context, _ string, u notification.UpdatePreferences) (notification.Preferences, error) {
					return notification.Preferences{UserID: "u1"}, nil
				},
			}, nil)

			_, err := svc.UpdatePreferences(context.Background(), "u1", tt.update)

			if tt.wantErr {
				ae := appErr(err)
				if ae == nil || ae.Code != apperror.ErrInvalidInput {
					t.Fatalf("err = %v, want INVALID_INPUT", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestService_UpdatePreferences_PassesThroughToRepo(t *testing.T) {
	var captured notification.UpdatePreferences
	svc := notification.NewService(&mockRepo{
		upsertPrefs: func(_ context.Context, _ string, u notification.UpdatePreferences) (notification.Preferences, error) {
			captured = u
			return notification.Preferences{
				UserID: "u1", EmailDigest: false, BeforeDueMinutes: 60,
			}, nil
		},
	}, nil)

	view, err := svc.UpdatePreferences(context.Background(), "u1", notification.UpdatePreferences{
		EmailDigest: ptrBool(false), BeforeDueMinutes: ptrInt(60),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.EmailDigest == nil || *captured.EmailDigest {
		t.Fatalf("emailDigest not passed through: %+v", captured)
	}
	if view.EmailDigest || view.BeforeDueMinutes != 60 {
		t.Fatalf("view = %+v, want emailDigest=false beforeDueMinutes=60", view)
	}
}
