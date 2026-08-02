// Package googlecal owns the user's Google Calendar connection (E-052).
//
// The refresh token is the sensitive asset here: it grants ongoing read access
// to every meeting title, attendee and location the user has, and read-only
// scope does not lower that stake. It is therefore encrypted at rest, never
// leaves this package as plaintext, and appears in no view type, log line or
// error message.
package googlecal

import "time"

// MaxSelectedCalendars caps how many calendars a user may overlay. Each selected
// calendar is a separate Google API call per ranged fetch, so this bounds the
// fan-out of a single calendar view (NIC-1857).
const MaxSelectedCalendars = 5

// Connection is the stored link between a Nicoflow user and their Google account.
//
// RefreshToken is the DECRYPTED token. It is typed Secret rather than string so
// that printing, logging or marshalling this struct — deliberately or by
// accident, including from a panic stack — yields [REDACTED] rather than live
// credentials. ConnectionView is what crosses the wire and carries no token at
// all.
type Connection struct {
	UserID string
	// RefreshToken is plaintext in memory only, never in a response or a log.
	RefreshToken        Secret
	GoogleAccountEmail  string
	SelectedCalendarIDs []string
	Scopes              []string
	ConnectedAt         time.Time
	LastSyncAt          *time.Time
	LastError           *string
}

// ConnectionView is the wire shape (SPEC §3). It carries no token field in any
// form — encrypted or otherwise — so there is no path by which a marshalled
// connection can leak one.
type ConnectionView struct {
	GoogleAccountEmail  string     `json:"googleAccountEmail"`
	SelectedCalendarIDs []string   `json:"selectedCalendarIds"`
	Scopes              []string   `json:"scopes"`
	ConnectedAt         time.Time  `json:"connectedAt"`
	LastSyncAt          *time.Time `json:"lastSyncAt"`
	LastError           *string    `json:"lastError"`
}

// View projects a Connection for the wire, dropping the refresh token.
func (c Connection) View() ConnectionView {
	// Nil slices marshal as null; the frontend expects arrays it can map over.
	selected := c.SelectedCalendarIDs
	if selected == nil {
		selected = []string{}
	}
	scopes := c.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	return ConnectionView{
		GoogleAccountEmail:  c.GoogleAccountEmail,
		SelectedCalendarIDs: selected,
		Scopes:              scopes,
		ConnectedAt:         c.ConnectedAt,
		LastSyncAt:          c.LastSyncAt,
		LastError:           c.LastError,
	}
}

// ConnectResponse carries the Google consent URL to an authenticated caller.
type ConnectResponse struct {
	AuthURL string `json:"authUrl"`
}
