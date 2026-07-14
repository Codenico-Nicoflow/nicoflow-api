package notification

import "time"

// Default preference values. Mirror the migration-032 column defaults; used by the
// read path when no row exists yet and by the upsert to fill absent fields on create.
const (
	DefaultEmailDigest      = true
	DefaultPushEnabled      = false
	DefaultSMSEnabled       = false
	DefaultBeforeDueMinutes = 1440 // 24h
	DefaultAfterDueMinutes  = 0

	// LeadTimeMax caps before/after lead times at one week, guarding against absurd
	// values the cron would otherwise honour. Validated in the service layer.
	LeadTimeMax = 10080 // 7 days in minutes
)

// Preferences is the internal per-user notification-preferences model.
type Preferences struct {
	UserID           string
	EmailDigest      bool
	PushEnabled      bool
	SMSEnabled       bool
	BeforeDueMinutes int
	AfterDueMinutes  int
	UpdatedAt        time.Time
}

// PreferencesView is the JSON response shape for the preferences endpoints.
type PreferencesView struct {
	EmailDigest      bool `json:"emailDigest"`
	PushEnabled      bool `json:"pushEnabled"`
	SMSEnabled       bool `json:"smsEnabled"`
	BeforeDueMinutes int  `json:"beforeDueMinutes"`
	AfterDueMinutes  int  `json:"afterDueMinutes"`
}

// UpdatePreferences is the partial/full update payload. Nil fields are left
// unchanged (or set to their default when the row is created by this upsert).
type UpdatePreferences struct {
	EmailDigest      *bool `json:"emailDigest"`
	PushEnabled      *bool `json:"pushEnabled"`
	SMSEnabled       *bool `json:"smsEnabled"`
	BeforeDueMinutes *int  `json:"beforeDueMinutes"`
	AfterDueMinutes  *int  `json:"afterDueMinutes"`
}

// defaultPreferences returns the preferences for a user with no stored row.
func defaultPreferences(userID string) Preferences {
	return Preferences{
		UserID:           userID,
		EmailDigest:      DefaultEmailDigest,
		PushEnabled:      DefaultPushEnabled,
		SMSEnabled:       DefaultSMSEnabled,
		BeforeDueMinutes: DefaultBeforeDueMinutes,
		AfterDueMinutes:  DefaultAfterDueMinutes,
	}
}

func preferencesToView(p Preferences) PreferencesView {
	return PreferencesView{
		EmailDigest:      p.EmailDigest,
		PushEnabled:      p.PushEnabled,
		SMSEnabled:       p.SMSEnabled,
		BeforeDueMinutes: p.BeforeDueMinutes,
		AfterDueMinutes:  p.AfterDueMinutes,
	}
}
