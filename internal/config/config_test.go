package config

import (
	"strings"
	"testing"
)

// setRequired sets the vars Load fatals without, so AI-specific cases can run.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", "test-jwt-secret-"+"test-jwt-secret-x") // 32 bytes, not a real key
	t.Setenv("PORT", "8080")
}

func TestLoad_AIConfig(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		model     string
		wantKey   string
		wantModel string
	}{
		{name: "key set, model default", key: "sk-ant-xxx", model: "", wantKey: "sk-ant-xxx", wantModel: defaultAIModel},
		{name: "key unset, model default", key: "", model: "", wantKey: "", wantModel: defaultAIModel},
		{name: "explicit model overrides default", key: "sk-ant-xxx", model: "claude-sonnet-5", wantKey: "sk-ant-xxx", wantModel: "claude-sonnet-5"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("ANTHROPIC_API_KEY", tc.key)
			t.Setenv("AI_MODEL", tc.model)

			cfg := Load()

			if cfg.AnthropicAPIKey != tc.wantKey {
				t.Errorf("AnthropicAPIKey = %q, want %q", cfg.AnthropicAPIKey, tc.wantKey)
			}
			if cfg.AIModel != tc.wantModel {
				t.Errorf("AIModel = %q, want %q", cfg.AIModel, tc.wantModel)
			}
		})
	}
}

func TestLoad_GoogleConfig(t *testing.T) {
	const (
		id       = "1234.apps.googleusercontent.com"
		secret   = "test-client-secret"
		redirect = "http://localhost:8080/v1/calendar/google/callback"
		// Config only checks presence, never decodes — so this is deliberately
		// not base64-shaped. A realistic-looking key here trips secret scanners.
		encKey = "not-a-real-key"
	)

	// All four are required together: credentials without the encryption key
	// would store a live refresh token in plaintext, so a half-configured
	// environment must read as "off", never as partially on.
	tests := []struct {
		name        string
		id          string
		secret      string
		redirect    string
		encKey      string
		wantEnabled bool
	}{
		{name: "all four set", id: id, secret: secret, redirect: redirect, encKey: encKey, wantEnabled: true},
		{name: "nothing set", wantEnabled: false},
		{name: "missing client id", secret: secret, redirect: redirect, encKey: encKey, wantEnabled: false},
		{name: "missing client secret", id: id, redirect: redirect, encKey: encKey, wantEnabled: false},
		{name: "missing redirect url", id: id, secret: secret, encKey: encKey, wantEnabled: false},
		{name: "missing encryption key", id: id, secret: secret, redirect: redirect, wantEnabled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("GOOGLE_CLIENT_ID", tt.id)
			t.Setenv("GOOGLE_CLIENT_SECRET", tt.secret)
			t.Setenv("GOOGLE_REDIRECT_URL", tt.redirect)
			t.Setenv("GOOGLE_TOKEN_ENC_KEY", tt.encKey)

			cfg := Load()

			if got := cfg.GoogleEnabled(); got != tt.wantEnabled {
				t.Errorf("GoogleEnabled() = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

// String() is what reaches a debug log; it must not carry either Google secret.
func TestConfig_StringRedactsGoogleSecrets(t *testing.T) {
	setRequired(t)
	t.Setenv("GOOGLE_CLIENT_SECRET", "super-secret-value")
	t.Setenv("GOOGLE_TOKEN_ENC_KEY", "super-secret-key")

	out := Load().String()

	for _, leaked := range []string{"super-secret-value", "super-secret-key"} {
		if strings.Contains(out, leaked) {
			t.Errorf("String() leaked %q: %s", leaked, out)
		}
	}
}
