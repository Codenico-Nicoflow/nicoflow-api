package project_test

import (
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

// frontendIcons mirrors the curated picker in the frontend
// (src/lib/types/icons.ts ICON_IDS). The backend AllowedIcons set MUST remain a
// superset of this list, or the UI can offer an icon the API rejects with
// INVALID_INPUT. This test is the regression guard against that drift.
var frontendIcons = []string{
	"folder", "briefcase", "user", "heart", "dollar-sign", "book",
	"lightbulb", "rocket", "palette", "music", "camera", "film", "gift",
	"calendar", "shopping-cart", "map", "plane", "tools", "code", "cpu",
	"shield", "trophy", "chat", "clipboard", "flag", "star", "leaf", "paw",
	"heart-pulse", "globe",
}

func TestAllowedIcons_SupersetOfFrontendPicker(t *testing.T) {
	for _, icon := range frontendIcons {
		if !project.AllowedIcons[icon] {
			t.Errorf("frontend icon %q is not accepted by backend AllowedIcons", icon)
		}
	}
}

func TestAllowedIcons_DefaultBriefcaseAccepted(t *testing.T) {
	// The frontend area schema defaults the icon to "briefcase"; it must be valid.
	if !project.AllowedIcons["briefcase"] {
		t.Error("default icon \"briefcase\" must be accepted by backend AllowedIcons")
	}
}

func TestAllowedIcons_UnknownRejected(t *testing.T) {
	if project.AllowedIcons["not-a-real-icon"] {
		t.Error("unknown icon should not be in AllowedIcons")
	}
}
