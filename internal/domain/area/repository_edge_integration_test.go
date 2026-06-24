//go:build integration

package area_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
)

// listAreas calls GET /v1/areas with an optional raw query string and decodes
// the paginated envelope.
func listAreas(t *testing.T, srv *httptest.Server, token, rawQuery string) area.ListAreasResponse {
	t.Helper()
	url := srv.URL + "/v1/areas"
	if rawQuery != "" {
		url += "?" + rawQuery
	}
	resp := do(t, http.MethodGet, url, token, nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)
	var env struct {
		Data area.ListAreasResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("listAreas decode: %v", err)
	}
	return env.Data
}

// ── cursor / pagination edges ──────────────────────────────────────────────────

func TestIntegration_Area_List_EmptySet(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")

	got := listAreas(t, srv, token, "")
	if len(got.Items) != 0 {
		t.Errorf("items: got %d, want 0", len(got.Items))
	}
	if got.NextCursor != "" {
		t.Errorf("nextCursor: got %q, want empty", got.NextCursor)
	}
}

func TestIntegration_Area_List_SingleItem_NoCursor(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")
	mustCreateArea(t, srv, token, "Only")

	got := listAreas(t, srv, token, "limit=10")
	if len(got.Items) != 1 {
		t.Fatalf("items: got %d, want 1", len(got.Items))
	}
	if got.NextCursor != "" {
		t.Errorf("nextCursor should be empty when all rows fit, got %q", got.NextCursor)
	}
}

func TestIntegration_Area_List_PageBoundary_CursorPaginates(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")
	for i := 0; i < 3; i++ {
		mustCreateArea(t, srv, token, fmt.Sprintf("Area-%d", i))
	}

	// limit exactly equals a full page → a cursor must be returned.
	page1 := listAreas(t, srv, token, "limit=2")
	if len(page1.Items) != 2 {
		t.Fatalf("page1 items: got %d, want 2", len(page1.Items))
	}
	if page1.NextCursor == "" {
		t.Fatal("page1 must return a nextCursor at the page boundary")
	}

	// follow the cursor → remaining row, no further cursor.
	page2 := listAreas(t, srv, token, "limit=2&cursor="+page1.NextCursor)
	if len(page2.Items) != 1 {
		t.Fatalf("page2 items: got %d, want 1", len(page2.Items))
	}
	if page2.NextCursor != "" {
		t.Errorf("page2 nextCursor should be empty, got %q", page2.NextCursor)
	}

	// pages must not overlap.
	seen := map[string]bool{}
	for _, it := range append(page1.Items, page2.Items...) {
		if seen[it.ID] {
			t.Errorf("duplicate id across pages: %s", it.ID)
		}
		seen[it.ID] = true
	}
}

func TestIntegration_Area_List_MalformedCursor_Returns400(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")

	// '!' is not in the base64 alphabet, so decodeCursor must fail cleanly.
	resp := do(t, http.MethodGet, srv.URL+"/v1/areas?cursor=not_valid_base64_%21%21", token, nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusBadRequest)
	assertErrorCode(t, resp, apperror.ErrInvalidInput)
}

func TestIntegration_Area_List_SearchWithCursor(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")
	mustCreateArea(t, srv, token, "Work Alpha")
	mustCreateArea(t, srv, token, "Work Beta")
	mustCreateArea(t, srv, token, "Personal")

	// q filters to the two "Work" areas; limit=1 forces a cursor across the filtered set.
	page1 := listAreas(t, srv, token, "q=Work&limit=1")
	if len(page1.Items) != 1 {
		t.Fatalf("page1 items: got %d, want 1", len(page1.Items))
	}
	if page1.NextCursor == "" {
		t.Fatal("filtered search must still paginate via cursor")
	}
	page2 := listAreas(t, srv, token, "q=Work&limit=1&cursor="+page1.NextCursor)
	if len(page2.Items) != 1 {
		t.Fatalf("page2 items: got %d, want 1", len(page2.Items))
	}
	// "Personal" must never appear in a q=Work search.
	for _, it := range append(page1.Items, page2.Items...) {
		if it.Name == "Personal" {
			t.Errorf("search leaked a non-matching area: %s", it.Name)
		}
	}
}

// ── reorder atomicity ──────────────────────────────────────────────────────────

func TestIntegration_Area_Reorder_NonExistentID_RollsBack(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")
	id1 := mustCreateArea(t, srv, token, "First")
	id2 := mustCreateArea(t, srv, token, "Second")

	// Batch contains one valid + one non-existent ID. The whole tx must roll back.
	body := area.ReorderRequest{Items: []area.ReorderItem{
		{ID: id1, DisplayOrder: 50},
		{ID: "does-not-exist", DisplayOrder: 99},
	}}
	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/reorder", token, body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, apperror.ErrAreaNotFound)

	// id1's display_order must be unchanged (rollback) — verify via DB.
	var order int
	if err := pool.QueryRow(t.Context(),
		`SELECT display_order FROM areas WHERE id = $1`, id1).Scan(&order); err != nil {
		t.Fatalf("query display_order: %v", err)
	}
	if order == 50 {
		t.Errorf("reorder was not rolled back: id1 display_order = 50")
	}
	_ = id2
}

func TestIntegration_Area_Reorder_DuplicateDisplayOrder_Succeeds(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")
	id1 := mustCreateArea(t, srv, token, "One")
	id2 := mustCreateArea(t, srv, token, "Two")

	// Sparse ordering allows duplicate display_order values; this must not error.
	body := area.ReorderRequest{Items: []area.ReorderItem{
		{ID: id1, DisplayOrder: 7},
		{ID: id2, DisplayOrder: 7},
	}}
	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/reorder", token, body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)
	var env struct {
		Data struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Updated != 2 {
		t.Errorf("updated: got %d, want 2", env.Data.Updated)
	}
}
