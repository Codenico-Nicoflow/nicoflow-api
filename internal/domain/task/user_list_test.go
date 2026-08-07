package task

import (
	"context"
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

func TestListForUser_ValidatesEnumsAndDates(t *testing.T) {
	badStatus := "sideways"
	badPriority := "urgent"
	badEnergy := "hyper"
	badFrom := "not-a-date"
	tests := []struct {
		name    string
		filter  UserListFilter
		wantErr string
	}{
		{"bad status", UserListFilter{Status: &badStatus}, apperror.ErrInvalidStatus},
		{"bad priority", UserListFilter{Priority: &badPriority}, apperror.ErrInvalidPriority},
		{"bad energy", UserListFilter{Energy: &badEnergy}, apperror.ErrInvalidInput},
		{"bad date", UserListFilter{ScheduledFrom: &badFrom}, apperror.ErrInvalidDate},
	}
	svc := NewService(&mockRepo{}, nil, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.ListForUser(context.Background(), "u1", tc.filter)
			ae := appErr(err)
			if ae == nil || ae.Code != tc.wantErr {
				t.Fatalf("got %v, want code %q", err, tc.wantErr)
			}
		})
	}
}

func TestListForUser_ProjectIDForeignBecomesProjectNotFound(t *testing.T) {
	pid := "p_missing"
	repo := &mockRepo{
		projectOwned: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	svc := NewService(repo, nil, nil)
	_, err := svc.ListForUser(context.Background(), "u1", UserListFilter{ProjectID: &pid})
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrProjectNotFound || ae.Status != http.StatusNotFound {
		t.Fatalf("want PROJECT_NOT_FOUND/404, got %v", err)
	}
}

func TestListForUser_LimitDefaultAndCap(t *testing.T) {
	tests := []struct {
		name    string
		in, out int
	}{
		{"zero → default", 0, userListDefaultLimit},
		{"negative → default", -3, userListDefaultLimit},
		{"in range preserved", 15, 15},
		{"over cap → cap", 500, userListMaxLimit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotLimit int
			repo := &mockRepo{
				listForUser: func(_ context.Context, _ string, f UserListFilter) ([]Task, error) {
					gotLimit = f.Limit
					return nil, nil
				},
			}
			svc := NewService(repo, nil, nil)
			if _, err := svc.ListForUser(context.Background(), "u1", UserListFilter{Limit: tc.in}); err != nil {
				t.Fatal(err)
			}
			if gotLimit != tc.out {
				t.Errorf("limit clamp: in=%d out=%d, want %d", tc.in, gotLimit, tc.out)
			}
		})
	}
}
