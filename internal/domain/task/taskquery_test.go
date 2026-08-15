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
		wantExpr    string
		wantDir     string
		wantArgKeys []string
	}{
		{
			name:        "default sort + base scope",
			filter:      ListTasksFilter{},
			wantInSQL:   []string{"user_id = @userID", "project_id = @projectID"},
			wantExpr:    "display_order",
			wantDir:     "ASC",
			wantArgKeys: []string{"userID", "projectID"},
		},
		{
			name:        "status + energy filters",
			filter:      ListTasksFilter{Status: ptr("active"), Energy: ptr("low")},
			wantInSQL:   []string{"status = @status", "energy = @energy"},
			wantExpr:    "display_order",
			wantDir:     "ASC",
			wantArgKeys: []string{"status", "energy"},
		},
		{
			name:        "search adds ILIKE on title+notes",
			filter:      ListTasksFilter{Search: "spec"},
			wantInSQL:   []string{"title ILIKE @search OR notes ILIKE @search"},
			wantExpr:    "display_order",
			wantDir:     "ASC",
			wantArgKeys: []string{"search"},
		},
		{
			name:     "sort by scheduledFor desc",
			filter:   ListTasksFilter{SortField: "scheduledFor", SortOrder: "desc"},
			wantExpr: "COALESCE(scheduled_for, '')",
			wantDir:  "DESC",
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
			whereSuffix, sort, dir, args, err := buildListQuery("u1", "p1", tt.filter)
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
				if !strings.Contains(whereSuffix, frag) {
					t.Errorf("WHERE missing %q\ngot: %s", frag, whereSuffix)
				}
			}
			if sort.Expr != tt.wantExpr {
				t.Errorf("sort.Expr = %q, want %q", sort.Expr, tt.wantExpr)
			}
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
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
