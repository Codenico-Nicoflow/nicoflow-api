package nlp

import (
	"context"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	pkgnlp "github.com/nicoflow/nicoflow-api/internal/pkg/nlp"
)

// Service parses natural-language date text. Stateless — no repository.
type Service interface {
	ParseDate(ctx context.Context, req ParseDateRequest) (ParseDateResponse, error)
}

type service struct{}

// NewService returns the default Service implementation.
func NewService() Service { return &service{} }

func (s *service) ParseDate(_ context.Context, req ParseDateRequest) (ParseDateResponse, error) {
	if err := validate(req); err != nil {
		return ParseDateResponse{}, err
	}

	loc, err := time.LoadLocation(req.Timezone)
	if err != nil {
		return ParseDateResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "timezone must be a valid IANA name")
	}

	now := time.Now().In(loc)

	result, err := pkgnlp.Parse(req.Locale, req.Text, now)
	if err != nil {
		return ParseDateResponse{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unable to parse text")
	}

	if !result.Matched {
		return ParseDateResponse{Confidence: confidenceLow}, nil
	}

	date := result.Date.Format("2006-01-02")
	display := result.Display
	return ParseDateResponse{
		Date:       &date,
		Confidence: confidenceHigh,
		Display:    &display,
	}, nil
}

func validate(req ParseDateRequest) error {
	if req.Text == "" || utf8.RuneCountInString(req.Text) > maxTextLen {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "text must be 1-100 characters")
	}
	if req.Timezone == "" || req.Timezone == "Local" {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "timezone must be a valid IANA name")
	}
	if !pkgnlp.SupportedLocales[req.Locale] {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "locale must be one of: en, ru")
	}
	return nil
}
