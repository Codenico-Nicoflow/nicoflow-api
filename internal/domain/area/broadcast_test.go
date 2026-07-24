package area_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/area"
)

type fakeBroadcaster struct{ events []area.Event }

func (f *fakeBroadcaster) Broadcast(_ string, ev area.Event) { f.events = append(f.events, ev) }

func TestBroadcast_EmitsCreateUpdateDelete(t *testing.T) {
	a := area.Area{ID: "a1", UserID: "u1", Name: "Home"}
	repo := &mockAreaRepo{
		countByUser: func(context.Context, string) (int, error) { return 0, nil },
		create:      func(_ context.Context, in area.Area) (area.Area, error) { return in, nil },
		update:      func(context.Context, string, string, area.UpdateAreaRequest) (area.Area, error) { return a, nil },
		delete:      func(context.Context, string, string) error { return nil },
	}
	fb := &fakeBroadcaster{}
	svc := area.NewService(repo, fb)

	if _, err := svc.Create(context.Background(), "u1", "pro", area.CreateAreaRequest{Name: "Home"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	name := "Work"
	if _, err := svc.Update(context.Background(), "u1", "a1", area.UpdateAreaRequest{Name: &name}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := svc.Delete(context.Background(), "u1", "a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := []string{area.EventCreated, area.EventUpdated, area.EventDeleted}
	if len(fb.events) != 3 {
		t.Fatalf("got %d events, want 3", len(fb.events))
	}
	for i, ev := range fb.events {
		if ev.Type != want[i] {
			t.Errorf("event[%d].Type = %q, want %q", i, ev.Type, want[i])
		}
	}
	if ref, ok := fb.events[2].Payload.(area.Ref); !ok || ref.ID != "a1" {
		t.Errorf("delete payload = %#v, want Ref{ID: a1}", fb.events[2].Payload)
	}
}

func TestBroadcast_NoEventOnFailedMutation(t *testing.T) {
	boom := errors.New("db down")
	repo := &mockAreaRepo{
		countByUser: func(context.Context, string) (int, error) { return 0, nil },
		create:      func(context.Context, area.Area) (area.Area, error) { return area.Area{}, boom },
		delete:      func(context.Context, string, string) error { return boom },
	}
	fb := &fakeBroadcaster{}
	svc := area.NewService(repo, fb)

	if _, err := svc.Create(context.Background(), "u1", "pro", area.CreateAreaRequest{Name: "Home"}); err == nil {
		t.Fatal("expected create error")
	}
	if err := svc.Delete(context.Background(), "u1", "a1"); err == nil {
		t.Fatal("expected delete error")
	}
	if len(fb.events) != 0 {
		t.Errorf("got %d events, want none on failed mutations", len(fb.events))
	}
}
