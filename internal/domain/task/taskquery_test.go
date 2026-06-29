package task

import (
	"strings"
	"testing"
)

func TestBuildListQuery(t *testing.T) {
	tests := []struct {
		name        string
		filter      ListTasksFilter
		wantErr     bool
		wantInSQL   []string
		wantArgKeys []string
	}{
		{
			name:        "default sort + base scope",
			filter:      ListTasksFilter{},
			wantInSQL:   []string{"user_id = @userID", "project_id = @projectID", "ORDER BY display_order ASC"},
			wantArgKeys: []string{"userID", "projectID"},
		},
		{
			name:        "status + energy filters",
			filter:      ListTasksFilter{Status: ptr("active"), Energy: ptr("low")},
			wantInSQL:   []string{"status = @status", "energy = @energy"},
			wantArgKeys: []string{"status", "energy"},
		},
		{
			name:        "search adds ILIKE on title+notes",
			filter:      ListTasksFilter{Search: "spec"},
			wantInSQL:   []string{"title ILIKE @search OR notes ILIKE @search"},
			wantArgKeys: []string{"search"},
		},
		{
			name:      "sort by dueDate desc",
			filter:    ListTasksFilter{SortField: "dueDate", SortOrder: "desc"},
			wantInSQL: []string{"ORDER BY due_date DESC"},
		},
		{
			name:    "unknown sortField rejected",
			filter:  ListTasksFilter{SortField: "bogus"},
			wantErr: true,
		},
		{
			name:    "bad sortOrder rejected",
			filter:  ListTasksFilter{SortOrder: "sideways"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := buildListQuery("u1", "p1", tt.filter)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, frag := range tt.wantInSQL {
				if !strings.Contains(sql, frag) {
					t.Errorf("SQL missing %q\ngot: %s", frag, sql)
				}
			}
			for _, k := range tt.wantArgKeys {
				if _, ok := args[k]; !ok {
					t.Errorf("args missing key %q", k)
				}
			}
			if v, ok := args["search"]; ok && tt.filter.Search != "" {
				if v != "%"+tt.filter.Search+"%" {
					t.Errorf("search arg = %v, want wildcarded", v)
				}
			}
		})
	}
}
