package googlecal

// Secret is a string that refuses to print itself.
//
// The Google refresh token grants ongoing read access to every meeting title,
// attendee and location a user has, so the goal is that it cannot leak even by
// accident. Scrubbing it at the reporter (a Sentry BeforeSend hook) only helps
// if the reporter is configured and the scrubber knows every shape the value
// might take — a panic can carry it in a stack value, a struct dump, a wrapped
// error, or a log field, and each is a separate thing to remember.
//
// Making the TYPE redact itself inverts that: Secret satisfies fmt.Stringer and
// encoding/json.Marshaler, which is what zerolog, %v/%s formatting, error
// wrapping and any crash reporter all funnel through. The plaintext is reachable
// only via an explicit Reveal() call, so every leak path becomes a visible,
// greppable decision rather than a default.
//
// This is deliberately not a substitute for a Sentry scrubber — it is the layer
// underneath one. When E-038 lands Sentry, a BeforeSend hook should still strip
// this package's values as defence in depth.
type Secret string

// String implements fmt.Stringer — covers %v, %s, log fields and stack dumps.
func (s Secret) String() string { return redacted }

// GoString implements fmt.GoStringer — covers %#v, which ignores String().
func (s Secret) GoString() string { return redacted }

// MarshalJSON ensures a Secret that reaches a JSON encoder — a response body, a
// structured log, or a crash-report payload — serializes as the placeholder.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// MarshalText covers encoders that prefer TextMarshaler over Stringer.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// Reveal returns the plaintext. Every call site is a deliberate decision to
// handle raw token material; there should be very few, and none of them should
// pass the result to anything that logs.
func (s Secret) Reveal() string { return string(s) }

// IsZero reports whether the secret is empty, without revealing it.
func (s Secret) IsZero() bool { return s == "" }

const redacted = "[REDACTED]"
