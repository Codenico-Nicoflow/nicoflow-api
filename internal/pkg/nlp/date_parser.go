// Package nlp wraps github.com/olebedev/when for stateless natural-language
// date parsing. No LLM fallback — EN/RU only, per NIC-1931.
package nlp

import (
	"fmt"
	"time"

	"github.com/olebedev/when"
)

// SupportedLocales is the allowlist for the locale field. Hebrew is
// intentionally excluded — olebedev/when has no Hebrew rule set, and a silent
// mis-parse is worse than a rejected request.
var SupportedLocales = map[string]bool{"en": true, "ru": true}

// ParseResult is the outcome of a date-parse attempt.
type ParseResult struct {
	// Date is the resolved point in time, in the caller's timezone. Zero
	// value if nothing matched.
	Date time.Time
	// Matched reports whether any rule matched. false ⇒ Date is meaningless.
	Matched bool
	// Display is the exact substring of the input that was recognized.
	Display string
}

// Parse resolves text against the given locale's rule set, using now (already
// converted into the caller's timezone) as the reference point for relative
// phrases like "next friday".
func Parse(locale, text string, now time.Time) (ParseResult, error) {
	parser, err := parserFor(locale)
	if err != nil {
		return ParseResult{}, err
	}

	res, err := parser.Parse(text, now)
	if err != nil {
		return ParseResult{}, fmt.Errorf("parse date text: %w", err)
	}
	if res == nil {
		return ParseResult{}, nil
	}

	return ParseResult{
		Date:    res.Time,
		Matched: true,
		Display: res.Text,
	}, nil
}

func parserFor(locale string) (*when.Parser, error) {
	switch locale {
	case "en":
		return when.EN, nil
	case "ru":
		return when.RU, nil
	default:
		return nil, fmt.Errorf("unsupported locale %q", locale)
	}
}
