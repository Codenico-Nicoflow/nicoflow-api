package attachment

import (
	"context"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/storage"
)

// ── fakes ────────────────────────────────────────────────────────────────────

type fakeRepo struct {
	inserted  bool
	insertErr error
	sumBytes  int64
	got       Attachment
	deleteErr error
	getResult Attachment
	getErr    error
}

func (f *fakeRepo) InsertGuarded(_ context.Context, a Attachment) (Attachment, bool, error) {
	f.got = a
	if f.insertErr != nil {
		return Attachment{}, false, f.insertErr
	}
	if !f.inserted {
		return Attachment{}, false, nil
	}
	return a, true, nil
}
func (f *fakeRepo) ListByOwner(context.Context, string, string, string) ([]Attachment, error) {
	return nil, nil
}
func (f *fakeRepo) GetByID(context.Context, string, string) (Attachment, error) {
	return f.getResult, f.getErr
}
func (f *fakeRepo) GetByS3Key(context.Context, string, string) (Attachment, error) {
	return f.getResult, f.getErr
}
func (f *fakeRepo) Delete(context.Context, string, string) error { return f.deleteErr }
func (f *fakeRepo) SumBytesForUser(context.Context, string) (int64, error) {
	return f.sumBytes, nil
}
func (f *fakeRepo) DeleteAllForOwner(context.Context, string, string, string) ([]Attachment, error) {
	return nil, nil
}
func (f *fakeRepo) ListByUser(context.Context, string) ([]Attachment, error) { return nil, nil }
func (f *fakeRepo) AllKeys(context.Context) (map[string]struct{}, error)     { return nil, nil }
func (f *fakeRepo) ListAllOwners(context.Context) ([]Owner, error)           { return nil, nil }
func (f *fakeRepo) DeleteByOwner(context.Context, string, string) ([]Attachment, error) {
	return nil, nil
}

type fakeStore struct {
	enabled    bool
	head       storage.HeadResult
	headErr    error
	deleted    []string
	presignErr error
}

func (f *fakeStore) Enabled() bool { return f.enabled }
func (f *fakeStore) PresignUpload(_, contentType string) (storage.PresignedUpload, error) {
	if f.presignErr != nil {
		return storage.PresignedUpload{}, f.presignErr
	}
	return storage.PresignedUpload{URL: "https://s3.test/bucket/obj", Headers: map[string]string{"Content-Type": contentType}}, nil
}
func (f *fakeStore) PresignDownload(context.Context, string, string) (string, error) {
	return "https://s3.test/download", nil
}
func (f *fakeStore) Head(context.Context, string) (storage.HeadResult, error) {
	return f.head, f.headErr
}
func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}
func (f *fakeStore) List(context.Context, string) ([]string, error) { return nil, nil }

type fakeOwners struct{ err error }

func (f fakeOwners) VerifyOwner(context.Context, string, string, string) error { return f.err }

type capBroadcaster struct{ events []Event }

func (c *capBroadcaster) Broadcast(_ string, ev Event) { c.events = append(c.events, ev) }

func code(t *testing.T, err error) string {
	t.Helper()
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		return ae.Code
	}
	t.Fatalf("expected AppError, got %v", err)
	return ""
}

const (
	userID = "u1"
	okKey  = "attachments/u1/task/t1/obj"
)

// ── UploadURL ────────────────────────────────────────────────────────────────

func TestUploadURL_GateOrder(t *testing.T) {
	tests := []struct {
		name     string
		plan     string
		enabled  bool
		ownerErr error
		req      UploadURLRequest
		wantCode string
	}{
		{
			name: "free plan blocked before anything else",
			plan: "free", enabled: false,
			req:      UploadURLRequest{OwnerType: "task", OwnerID: "t1", FileName: "f.pdf", MimeType: "application/pdf"},
			wantCode: apperror.ErrPlanLimitExceeded,
		},
		{
			name: "storage disabled after plan passes",
			plan: planPro, enabled: false,
			req:      UploadURLRequest{OwnerType: "task", OwnerID: "t1", FileName: "f.pdf", MimeType: "application/pdf"},
			wantCode: apperror.ErrServiceUnavailable,
		},
		{
			name: "foreign owner rejected as not-found",
			plan: planPro, enabled: true, ownerErr: apperror.New(404, apperror.ErrResourceNotFound, "x"),
			req:      UploadURLRequest{OwnerType: "task", OwnerID: "t1", FileName: "f.pdf", MimeType: "application/pdf"},
			wantCode: apperror.ErrResourceNotFound,
		},
		{
			name: "unknown owner type is invalid input",
			plan: planPro, enabled: true,
			req:      UploadURLRequest{OwnerType: "note", OwnerID: "n1", FileName: "f.pdf", MimeType: "application/pdf"},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name: "disallowed mime rejected",
			plan: planPro, enabled: true,
			req:      UploadURLRequest{OwnerType: "task", OwnerID: "t1", FileName: "e.svg", MimeType: "image/svg+xml"},
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name: "oversize claim rejected",
			plan: planPro, enabled: true,
			req:      UploadURLRequest{OwnerType: "task", OwnerID: "t1", FileName: "f.pdf", MimeType: "application/pdf", ClaimedSize: maxUploadBytes + 1},
			wantCode: apperror.ErrInvalidInput,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(&fakeRepo{}, &fakeStore{enabled: tc.enabled}, fakeOwners{err: tc.ownerErr}, nil, nil)
			_, err := svc.UploadURL(context.Background(), userID, tc.plan, tc.req)
			if got := code(t, err); got != tc.wantCode {
				t.Fatalf("code = %q, want %q", got, tc.wantCode)
			}
		})
	}
}

func TestUploadURL_Success(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeStore{enabled: true}, fakeOwners{}, nil, nil)
	resp, err := svc.UploadURL(context.Background(), userID, planPro,
		UploadURLRequest{OwnerType: "task", OwnerID: "t1", FileName: "f.pdf", MimeType: "application/pdf", ClaimedSize: 1024})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.S3Key == "" || resp.URL == "" {
		t.Fatalf("empty presign response: %+v", resp)
	}
}

// ── Confirm ──────────────────────────────────────────────────────────────────

func TestConfirm_HeadObjectRevalidates(t *testing.T) {
	// Client uploaded a real object whose true type is disallowed: HeadObject must
	// catch it, the object gets deleted, and no row is written.
	store := &fakeStore{enabled: true, head: storage.HeadResult{ContentType: "image/svg+xml", ContentLength: 10}}
	repo := &fakeRepo{inserted: true}
	svc := NewService(repo, store, fakeOwners{}, nil, nil)

	_, err := svc.Confirm(context.Background(), userID, planPro, ConfirmRequest{S3Key: okKey, FileName: "x.png"})
	if got := code(t, err); got != apperror.ErrInvalidInput {
		t.Fatalf("code = %q, want INVALID_INPUT", got)
	}
	if len(store.deleted) != 1 || store.deleted[0] != okKey {
		t.Fatalf("orphan object not deleted: %v", store.deleted)
	}
}

func TestConfirm_StoresRealMetadataNotClaimed(t *testing.T) {
	// HeadObject reports 2048/pdf; that must be what is persisted regardless of any
	// client claim.
	store := &fakeStore{enabled: true, head: storage.HeadResult{ContentType: "application/pdf", ContentLength: 2048}}
	repo := &fakeRepo{inserted: true}
	bc := &capBroadcaster{}
	svc := NewService(repo, store, fakeOwners{}, nil, bc)

	view, err := svc.Confirm(context.Background(), userID, planPro, ConfirmRequest{S3Key: okKey, FileName: "report.pdf"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.got.FileSize != 2048 || repo.got.MimeType != "application/pdf" {
		t.Fatalf("stored claimed metadata, not S3 truth: %+v", repo.got)
	}
	if view.FileSize != 2048 {
		t.Fatalf("view size = %d, want 2048", view.FileSize)
	}
	if len(bc.events) != 1 || bc.events[0].Type != EventCreated {
		t.Fatalf("want one attachment.created event, got %+v", bc.events)
	}
}

func TestConfirm_ForeignKeyPrefixIsNotFound(t *testing.T) {
	store := &fakeStore{enabled: true}
	svc := NewService(&fakeRepo{}, store, fakeOwners{}, nil, nil)
	_, err := svc.Confirm(context.Background(), userID, planPro, ConfirmRequest{S3Key: "attachments/other/task/t1/obj", FileName: "x"})
	if got := code(t, err); got != apperror.ErrResourceNotFound {
		t.Fatalf("code = %q, want RESOURCE_NOT_FOUND", got)
	}
}

func TestConfirm_QuotaErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		sumBytes int64
		wantCode string
	}{
		{
			name:     "byte cap exceeded maps to storage limit",
			sumBytes: MaxBytesPerUser, // already full → incoming pushes over
			wantCode: apperror.ErrStorageLimitExceeded,
		},
		{
			name:     "count cap (bytes fine) maps to plan limit",
			sumBytes: 0,
			wantCode: apperror.ErrPlanLimitExceeded,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{enabled: true, head: storage.HeadResult{ContentType: "application/pdf", ContentLength: 1024}}
			repo := &fakeRepo{inserted: false, sumBytes: tc.sumBytes} // guard trips
			svc := NewService(repo, store, fakeOwners{}, nil, nil)
			_, err := svc.Confirm(context.Background(), userID, planPro, ConfirmRequest{S3Key: okKey, FileName: "f.pdf"})
			if got := code(t, err); got != tc.wantCode {
				t.Fatalf("code = %q, want %q", got, tc.wantCode)
			}
			if len(store.deleted) != 1 {
				t.Fatalf("over-quota object must be deleted, got %v", store.deleted)
			}
		})
	}
}

func TestConfirm_FreeBlocked(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeStore{enabled: true}, fakeOwners{}, nil, nil)
	_, err := svc.Confirm(context.Background(), userID, "free", ConfirmRequest{S3Key: okKey})
	if got := code(t, err); got != apperror.ErrPlanLimitExceeded {
		t.Fatalf("code = %q, want PLAN_LIMIT_EXCEEDED", got)
	}
}

// ── reads / delete: open on any plan ─────────────────────────────────────────

func TestDelete_OpenOnFreePlan_EmitsEvent(t *testing.T) {
	store := &fakeStore{enabled: true}
	repo := &fakeRepo{getResult: Attachment{ID: "a1", UserID: userID, S3Key: okKey}}
	bc := &capBroadcaster{}
	svc := NewService(repo, store, fakeOwners{}, nil, bc)

	if err := svc.Delete(context.Background(), userID, "a1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != okKey {
		t.Fatalf("S3 object not deleted: %v", store.deleted)
	}
	if len(bc.events) != 1 || bc.events[0].Type != EventDeleted {
		t.Fatalf("want attachment.deleted event, got %+v", bc.events)
	}
	got, ok := bc.events[0].Payload.(DeletedPayload)
	if !ok || got.ID != "a1" {
		t.Fatalf("deleted payload = %+v, want DeletedPayload{ID:a1,...}", bc.events[0].Payload)
	}
}

// panicBroadcaster models a broken transport: every broadcast panics.
type panicBroadcaster struct{}

func (panicBroadcaster) Broadcast(string, Event) { panic("hub down") }

func TestConfirm_BroadcastPanicDoesNotFailMutation(t *testing.T) {
	store := &fakeStore{enabled: true, head: storage.HeadResult{ContentType: "application/pdf", ContentLength: 1024}}
	repo := &fakeRepo{inserted: true}
	svc := NewService(repo, store, fakeOwners{}, nil, panicBroadcaster{})

	view, err := svc.Confirm(context.Background(), userID, planPro, ConfirmRequest{S3Key: okKey, FileName: "f.pdf"})
	if err != nil {
		t.Fatalf("broadcast panic must not fail confirm, got %v", err)
	}
	if view.FileSize != 1024 {
		t.Fatalf("view not returned after broadcast panic: %+v", view)
	}
}

func TestDelete_BroadcastPanicDoesNotFailMutation(t *testing.T) {
	store := &fakeStore{enabled: true}
	repo := &fakeRepo{getResult: Attachment{ID: "a1", UserID: userID, S3Key: okKey}}
	svc := NewService(repo, store, fakeOwners{}, nil, panicBroadcaster{})

	if err := svc.Delete(context.Background(), userID, "a1"); err != nil {
		t.Fatalf("broadcast panic must not fail delete, got %v", err)
	}
	if len(store.deleted) != 1 {
		t.Fatalf("delete side effects must still run, got %v", store.deleted)
	}
}

func TestDelete_NotFoundPropagates(t *testing.T) {
	repo := &fakeRepo{getErr: apperror.New(404, apperror.ErrResourceNotFound, "x")}
	svc := NewService(repo, &fakeStore{enabled: true}, fakeOwners{}, nil, nil)
	if got := code(t, svc.Delete(context.Background(), userID, "missing")); got != apperror.ErrResourceNotFound {
		t.Fatalf("code = %q, want RESOURCE_NOT_FOUND", got)
	}
}

// ── unit helpers ─────────────────────────────────────────────────────────────

func TestSanitizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"  spaced.txt  ", "spaced.txt"},
		{"a/b\\c.png", "a_b_c.png"},
		{"   ", "file"},
		{"", "file"},
		{"a\nb.pdf", "ab.pdf"},
		{"a\tb.pdf", "ab.pdf"},
		{"a\x00b.pdf", "ab.pdf"},
		{"\n\t", "file"},
	}
	for _, tc := range tests {
		if got := sanitizeName(tc.in); got != tc.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStorageUsage(t *testing.T) {
	repo := &fakeRepo{sumBytes: 1234}
	svc := NewService(repo, &fakeStore{enabled: true}, fakeOwners{}, nil, nil)

	got, err := svc.StorageUsage(context.Background(), userID)
	if err != nil {
		t.Fatalf("StorageUsage: %v", err)
	}
	if got.UsedBytes != 1234 {
		t.Errorf("UsedBytes = %d, want 1234", got.UsedBytes)
	}
	if got.LimitBytes != MaxBytesPerUser {
		t.Errorf("LimitBytes = %d, want %d", got.LimitBytes, MaxBytesPerUser)
	}
}

func TestOwnerFromKey(t *testing.T) {
	ot, oid, ok := ownerFromKey(userID, okKey)
	if !ok || ot != "task" || oid != "t1" {
		t.Fatalf("ownerFromKey = %q/%q ok=%v", ot, oid, ok)
	}
	if _, _, ok := ownerFromKey(userID, "attachments/u1/task"); ok {
		t.Fatalf("malformed key should not parse")
	}
}

func TestIsAllowedMime(t *testing.T) {
	if !isAllowedMime("application/pdf") {
		t.Fatal("pdf must be allowed")
	}
	if isAllowedMime("image/svg+xml") {
		t.Fatal("svg must be rejected")
	}
}
