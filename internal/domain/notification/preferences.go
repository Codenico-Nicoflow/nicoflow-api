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

	// Per-family toggles (E-025) default on: existing users keep every family until
	// they opt out. Mirror the migration-034 column defaults.
	DefaultOverdueEnabled      = true
	DefaultDailySummaryEnabled = true
	DefaultInboxNudgesEnabled  = true
	DefaultStreaksEnabled      = true

	// LeadTimeMax caps before/after lead times at one week, guarding against absurd
	// values the cron would otherwise honour. Validated in the service layer.
	LeadTimeMax = 10080 // 7 days in minutes
)

// Preferences is the internal per-user notification-preferences model.
type Preferences struct {
	UserID              string
	EmailDigest         bool
	PushEnabled         bool
	SMSEnabled          bool
	BeforeDueMinutes    int
	AfterDueMinutes     int
	OverdueEnabled      bool
	DailySummaryEnabled bool
	InboxNudgesEnabled  bool
	StreaksEnabled      bool
	UpdatedAt           time.Time
}

// PreferencesView is the JSON response shape for the preferences endpoints.
type PreferencesView struct {
	EmailDigest         bool `json:"emailDigest"`
	PushEnabled         bool `json:"pushEnabled"`
	SMSEnabled          bool `json:"smsEnabled"`
	BeforeDueMinutes    int  `json:"beforeDueMinutes"`
	AfterDueMinutes     int  `json:"afterDueMinutes"`
	OverdueEnabled      bool `json:"overdueEnabled"`
	DailySummaryEnabled bool `json:"dailySummaryEnabled"`
	InboxNudgesEnabled  bool `json:"inboxNudgesEnabled"`
	StreaksEnabled      bool `json:"streaksEnabled"`
}

// UpdatePreferences is the partial/full update payload. Nil fields are left
// unchanged (or set to their default when the row is created by this upsert).
type UpdatePreferences struct {
	EmailDigest         *bool `json:"emailDigest"`
	PushEnabled         *bool `json:"pushEnabled"`
	SMSEnabled          *bool `json:"smsEnabled"`
	BeforeDueMinutes    *int  `json:"beforeDueMinutes"`
	AfterDueMinutes     *int  `json:"afterDueMinutes"`
	OverdueEnabled      *bool `json:"overdueEnabled"`
	DailySummaryEnabled *bool `json:"dailySummaryEnabled"`
	InboxNudgesEnabled  *bool `json:"inboxNudgesEnabled"`
	StreaksEnabled      *bool `json:"streaksEnabled"`
}

// defaultPreferences returns the preferences for a user with no stored row.
func defaultPreferences(userID string) Preferences {
	return Preferences{
		UserID:              userID,
		EmailDigest:         DefaultEmailDigest,
		PushEnabled:         DefaultPushEnabled,
		SMSEnabled:          DefaultSMSEnabled,
		BeforeDueMinutes:    DefaultBeforeDueMinutes,
		AfterDueMinutes:     DefaultAfterDueMinutes,
		OverdueEnabled:      DefaultOverdueEnabled,
		DailySummaryEnabled: DefaultDailySummaryEnabled,
		InboxNudgesEnabled:  DefaultInboxNudgesEnabled,
		StreaksEnabled:      DefaultStreaksEnabled,
	}
}

func preferencesToView(p Preferences) PreferencesView {
	return PreferencesView{
		EmailDigest:         p.EmailDigest,
		PushEnabled:         p.PushEnabled,
		SMSEnabled:          p.SMSEnabled,
		BeforeDueMinutes:    p.BeforeDueMinutes,
		AfterDueMinutes:     p.AfterDueMinutes,
		OverdueEnabled:      p.OverdueEnabled,
		DailySummaryEnabled: p.DailySummaryEnabled,
		InboxNudgesEnabled:  p.InboxNudgesEnabled,
		StreaksEnabled:      p.StreaksEnabled,
	}
}
