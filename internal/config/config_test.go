package config

import "testing"

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
