package config

import "testing"

func TestParseInt(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		fallback int
		want     int
	}{
		{"empty falls back", "", 60, 60},
		{"valid int", "300", 60, 300},
		{"invalid falls back", "abc", 60, 60},
		{"zero falls back", "0", 60, 60},
		{"negative falls back", "-5", 60, 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInt(tt.in, tt.fallback); got != tt.want {
				t.Errorf("parseInt(%q, %d) = %d, want %d", tt.in, tt.fallback, got, tt.want)
			}
		})
	}
}
