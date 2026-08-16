// Package nlp exposes a stateless natural-language date-parsing endpoint.
// No repository — nothing here is persisted. See internal/pkg/nlp for the
// olebedev/when wrapper this domain calls into.
package nlp

// ParseDateRequest is the request body for POST /nlp/parse-date.
type ParseDateRequest struct {
	Text     string `json:"text"`
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
}

// ParseDateResponse is the wire shape returned on every successful (200) call.
// Unparseable input is not an error — it comes back with a nil Date and
// Confidence "low".
type ParseDateResponse struct {
	Date       *string `json:"date"`
	Confidence string  `json:"confidence"`
	Display    *string `json:"display"`
}

const (
	confidenceHigh = "high"
	confidenceLow  = "low"

	maxTextLen = 100
)
