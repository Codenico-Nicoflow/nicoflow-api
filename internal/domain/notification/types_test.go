package notification

import (
	"testing"
)

func TestCategoryForType(t *testing.T) {
	cases := []struct {
		notifType string
		want      string
	}{
		{TypeTaskDueSoon, CategoryReminder},
		{TypeTaskOverdue, CategoryReminder},
		{TypeTaskScheduledToday, CategoryReminder},
		{TypeNothingScheduled, CategoryReminder},
		{TypeInboxUnprocessed, CategoryReminder},
		{TypeInboxStale, CategoryReminder},
		{TypeDailySummary, CategorySummary},
		{TypeTaskCompleted, CategoryCelebration},
		{TypeProjectCompleted, CategoryCelebration},
		{TypeInboxZero, CategoryCelebration},
		{TypeStreakMilestone, CategoryCelebration},
		{TypeSystemAnnouncement, CategorySystem},
		// Unknown types fall back to system.
		{"some_future_type", CategorySystem},
		{"", CategorySystem},
	}

	for _, tc := range cases {
		t.Run(tc.notifType, func(t *testing.T) {
			got := categoryForType(tc.notifType)
			if got != tc.want {
				t.Errorf("categoryForType(%q) = %q, want %q", tc.notifType, got, tc.want)
			}
		})
	}
}

func TestNotificationToView_Category(t *testing.T) {
	n := Notification{
		ID:    "n1",
		Type:  TypeTaskCompleted,
		Title: "Done",
		Body:  "Task completed.",
	}
	v := notificationToView(n)
	if v.Category != CategoryCelebration {
		t.Errorf("notificationToView category = %q, want %q", v.Category, CategoryCelebration)
	}
}
