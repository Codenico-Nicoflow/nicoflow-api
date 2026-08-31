package notification

import "time"

// Default preference values. Mirror the migration-032/052 column defaults; used
// by the read path when no row exists yet and by the upsert to fill absent
// fields on create.
const (
	DefaultEmailDigest = true
	DefaultPushEnabled = false
	DefaultSMSEnabled  = false

	// Digest toggles (notification rework, 2026-08-31) default on: existing users
	// keep receiving both digests until they opt out. Mirror the migration-052
	// column defaults. streaksEnabled is untouched by the rework.
	DefaultMorningDigestEnabled = true
	DefaultEveningDigestEnabled = true
	DefaultStreaksEnabled       = true

	// Reminder hours (E-025, NIC-1627). Mirror the migration-035 column defaults +
	// CHECK ranges. morning_hour drives the morning digest; evening_hour the
	// evening digest. Ranges are validated in the service layer AND enforced by
	// the DB CHECK.
	DefaultMorningHour = 8
	DefaultEveningHour = 20
	MorningHourMin     = 5
	MorningHourMax     = 11
	EveningHourMin     = 18
	EveningHourMax     = 22
)

// Preferences is the internal per-user notification-preferences model.
type Preferences struct {
	UserID               string
	EmailDigest          bool
	PushEnabled          bool
	SMSEnabled           bool
	MorningDigestEnabled bool
	EveningDigestEnabled bool
	StreaksEnabled       bool
	MorningHour          int
	EveningHour          int
	UpdatedAt            time.Time
}

// PreferencesView is the JSON response shape for the preferences endpoints.
type PreferencesView struct {
	EmailDigest          bool `json:"emailDigest"`
	PushEnabled          bool `json:"pushEnabled"`
	SMSEnabled           bool `json:"smsEnabled"`
	MorningDigestEnabled bool `json:"morningDigestEnabled"`
	EveningDigestEnabled bool `json:"eveningDigestEnabled"`
	StreaksEnabled       bool `json:"streaksEnabled"`
	MorningHour          int  `json:"morningHour"`
	EveningHour          int  `json:"eveningHour"`
}

// UpdatePreferences is the partial/full update payload. Nil fields are left
// unchanged (or set to their default when the row is created by this upsert).
type UpdatePreferences struct {
	EmailDigest          *bool `json:"emailDigest"`
	PushEnabled          *bool `json:"pushEnabled"`
	SMSEnabled           *bool `json:"smsEnabled"`
	MorningDigestEnabled *bool `json:"morningDigestEnabled"`
	EveningDigestEnabled *bool `json:"eveningDigestEnabled"`
	StreaksEnabled       *bool `json:"streaksEnabled"`
	MorningHour          *int  `json:"morningHour"`
	EveningHour          *int  `json:"eveningHour"`
}

// defaultPreferences returns the preferences for a user with no stored row.
func defaultPreferences(userID string) Preferences {
	return Preferences{
		UserID:               userID,
		EmailDigest:          DefaultEmailDigest,
		PushEnabled:          DefaultPushEnabled,
		SMSEnabled:           DefaultSMSEnabled,
		MorningDigestEnabled: DefaultMorningDigestEnabled,
		EveningDigestEnabled: DefaultEveningDigestEnabled,
		StreaksEnabled:       DefaultStreaksEnabled,
		MorningHour:          DefaultMorningHour,
		EveningHour:          DefaultEveningHour,
	}
}

func preferencesToView(p Preferences) PreferencesView {
	return PreferencesView{
		EmailDigest:          p.EmailDigest,
		PushEnabled:          p.PushEnabled,
		SMSEnabled:           p.SMSEnabled,
		MorningDigestEnabled: p.MorningDigestEnabled,
		EveningDigestEnabled: p.EveningDigestEnabled,
		StreaksEnabled:       p.StreaksEnabled,
		MorningHour:          p.MorningHour,
		EveningHour:          p.EveningHour,
	}
}
