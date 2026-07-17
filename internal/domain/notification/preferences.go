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

	// Reminder hours (E-025, NIC-1627). Mirror the migration-035 column defaults +
	// CHECK ranges. morning_hour drives the morning sweeps; evening_hour the summary.
	// Ranges are validated in the service layer AND enforced by the DB CHECK.
	DefaultMorningHour = 8
	DefaultEveningHour = 20
	MorningHourMin     = 5
	MorningHourMax     = 11
	EveningHourMin     = 18
	EveningHourMax     = 22
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
	MorningHour         int
	EveningHour         int
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
	MorningHour         int  `json:"morningHour"`
	EveningHour         int  `json:"eveningHour"`
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
	MorningHour         *int  `json:"morningHour"`
	EveningHour         *int  `json:"eveningHour"`
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
		MorningHour:         DefaultMorningHour,
		EveningHour:         DefaultEveningHour,
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
		MorningHour:         p.MorningHour,
		EveningHour:         p.EveningHour,
	}
}
