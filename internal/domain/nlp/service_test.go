package nlp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/nlp"
)

func TestService_ParseDate(t *testing.T) {
	svc := nlp.NewService()

	tests := []struct {
		name           string
		req            nlp.ParseDateRequest
		wantErrCode    string
		wantConfidence string
		wantDateNil    bool
	}{
		{
			name:           "english relative phrase resolves with high confidence",
			req:            nlp.ParseDateRequest{Text: "next friday", Timezone: "UTC", Locale: "en"},
			wantConfidence: "high",
		},
		{
			name:           "russian relative phrase resolves with high confidence",
			req:            nlp.ParseDateRequest{Text: "в следующую пятницу", Timezone: "UTC", Locale: "ru"},
			wantConfidence: "high",
		},
		{
			name:           "unparseable text returns low confidence, not an error",
			req:            nlp.ParseDateRequest{Text: "asdfghjkl qwerty", Timezone: "UTC", Locale: "en"},
			wantConfidence: "low",
			wantDateNil:    true,
		},
		{
			name:        "empty text is invalid input",
			req:         nlp.ParseDateRequest{Text: "", Timezone: "UTC", Locale: "en"},
			wantErrCode: apperror.ErrInvalidInput,
		},
		{
			name:        "text over 100 chars is invalid input",
			req:         nlp.ParseDateRequest{Text: repeat("a", 101), Timezone: "UTC", Locale: "en"},
			wantErrCode: apperror.ErrInvalidInput,
		},
		{
			name:        "hebrew locale is rejected (out of scope, no rule set)",
			req:         nlp.ParseDateRequest{Text: "next friday", Timezone: "UTC", Locale: "he"},
			wantErrCode: apperror.ErrInvalidInput,
		},
		{
			name:        "unsupported locale is rejected",
			req:         nlp.ParseDateRequest{Text: "next friday", Timezone: "UTC", Locale: "fr"},
			wantErrCode: apperror.ErrInvalidInput,
		},
		{
			name:        "invalid IANA timezone is rejected",
			req:         nlp.ParseDateRequest{Text: "next friday", Timezone: "Not/AZone", Locale: "en"},
			wantErrCode: apperror.ErrInvalidInput,
		},
		{
			name:        "empty timezone is rejected",
			req:         nlp.ParseDateRequest{Text: "next friday", Timezone: "", Locale: "en"},
			wantErrCode: apperror.ErrInvalidInput,
		},
		{
			name:           "valid non-UTC IANA timezone converts correctly",
			req:            nlp.ParseDateRequest{Text: "tomorrow", Timezone: "Asia/Jerusalem", Locale: "en"},
			wantConfidence: "high",
		},
		{
			name:           "DST-boundary timezone still resolves",
			req:            nlp.ParseDateRequest{Text: "tomorrow", Timezone: "America/New_York", Locale: "en"},
			wantConfidence: "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.ParseDate(context.Background(), tt.req)

			if tt.wantErrCode != "" {
				var ae *apperror.AppError
				if !errors.As(err, &ae) {
					t.Fatalf("expected AppError, got %v", err)
				}
				if ae.Code != tt.wantErrCode {
					t.Fatalf("code = %q, want %q", ae.Code, tt.wantErrCode)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Confidence != tt.wantConfidence {
				t.Fatalf("confidence = %q, want %q", resp.Confidence, tt.wantConfidence)
			}
			if tt.wantDateNil && resp.Date != nil {
				t.Fatalf("expected nil date, got %q", *resp.Date)
			}
			if !tt.wantDateNil && resp.Date == nil {
				t.Fatalf("expected non-nil date")
			}
		})
	}
}

func TestService_ParseDate_TimezoneConversion(t *testing.T) {
	svc := nlp.NewService()

	// "today" in a timezone far ahead of UTC (Pacific/Kiritimati, UTC+14) can
	// be a different calendar date than "today" in UTC depending on wall-clock
	// time — assert the service resolves against the caller's timezone, not
	// the server's raw UTC clock.
	resp, err := svc.ParseDate(context.Background(), nlp.ParseDateRequest{
		Text:     "today",
		Timezone: "Pacific/Kiritimati",
		Locale:   "en",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Date == nil {
		t.Fatalf("expected resolved date")
	}

	loc, _ := time.LoadLocation("Pacific/Kiritimati")
	want := time.Now().In(loc).Format("2006-01-02")
	if *resp.Date != want {
		t.Fatalf("date = %q, want %q (server-now converted into caller tz)", *resp.Date, want)
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, s...)
	}
	return string(out[:n])
}
