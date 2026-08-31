package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/project"
)

type fakeBroadcaster struct{ events []project.Event }

func (f *fakeBroadcaster) Broadcast(_ string, ev project.Event) { f.events = append(f.events, ev) }

func TestBroadcast_EmitsCreateUpdateDelete(t *testing.T) {
	p := project.Project{ID: "p1", UserID: "u1", AreaID: "a1", Name: "Site", Status: "active"}
	repo := &mockProjectRepo{
		countByUser: func(context.Context, string) (int, error) { return 0, nil },
		create:      func(_ context.Context, in project.Project) (project.Project, error) { return in, nil },
		update: func(context.Context, string, string, project.UpdateProjectRequest) (project.Project, error) {
			return p, nil
		},
		delete: func(context.Context, string, string) error { return nil },
	}
	fb := &fakeBroadcaster{}
	svc := project.NewService(repo, fb, nil)

	if _, err := svc.Create(context.Background(), "u1", "a1", "pro", project.CreateProjectRequest{Name: "Site"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := svc.Update(context.Background(), "u1", "p1", project.UpdateProjectRequest{Name: strPtr("Site2")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := svc.Delete(context.Background(), "u1", "p1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := []string{project.EventCreated, project.EventUpdated, project.EventDeleted}
	if len(fb.events) != 3 {
		t.Fatalf("got %d events, want 3", len(fb.events))
	}
	for i, ev := range fb.events {
		if ev.Type != want[i] {
			t.Errorf("event[%d].Type = %q, want %q", i, ev.Type, want[i])
		}
	}
	if ref, ok := fb.events[2].Payload.(project.Ref); !ok || ref.ID != "p1" {
		t.Errorf("delete payload = %#v, want Ref{ID: p1}", fb.events[2].Payload)
	}
}

func TestBroadcast_NoEventOnFailedMutation(t *testing.T) {
	boom := errors.New("db down")
	repo := &mockProjectRepo{
		countByUser: func(context.Context, string) (int, error) { return 0, nil },
		create:      func(context.Context, project.Project) (project.Project, error) { return project.Project{}, boom },
		delete:      func(context.Context, string, string) error { return boom },
	}
	fb := &fakeBroadcaster{}
	svc := project.NewService(repo, fb, nil)

	if _, err := svc.Create(context.Background(), "u1", "a1", "pro", project.CreateProjectRequest{Name: "Site"}); err == nil {
		t.Fatal("expected create error")
	}
	if err := svc.Delete(context.Background(), "u1", "p1"); err == nil {
		t.Fatal("expected delete error")
	}
	if len(fb.events) != 0 {
		t.Errorf("got %d events, want none on failed mutations", len(fb.events))
	}
}
