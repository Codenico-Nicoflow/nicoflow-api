package note

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

type service struct {
	repo        Repository
	projects    ProjectOwnershipVerifier
	broadcaster Broadcaster       // nil disables emission
	cleaner     AttachmentCleaner // nil disables attachment cleanup
}

// NewService creates a note Service. Notes are Free and unlimited, so there is
// no plan parameter anywhere in this domain — adding a cap later would be a
// deliberate contract change, not an oversight. broadcaster may be nil
// (real-time emission disabled).
func NewService(repo Repository, projects ProjectOwnershipVerifier, broadcaster Broadcaster) Service {
	return &service{repo: repo, projects: projects, broadcaster: broadcaster}
}

// WithCleaner returns the service with attachment cleanup enabled. Wired
// post-construction because the attachment service already depends on this one
// via OwnerVerifier — the concretes meet only in main.go, so the cycle stays
// acyclic.
func (s *service) WithCleaner(c AttachmentCleaner) Service {
	s.cleaner = c
	return s
}

// emit fans a domain event out best-effort. A nil broadcaster is a valid no-op.
func (s *service) emit(userID string, ev Event) {
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(userID, ev)
	}
}

// cleanAttachments best-effort removes a deleted note's attachments. A failure is
// logged and swallowed: the note is already gone, and the GC sweep reconciles
// anything left behind, so cleanup must never fail the delete the caller saw
// succeed.
func (s *service) cleanAttachments(ctx context.Context, userID, noteID string) {
	if s.cleaner == nil {
		return
	}
	if err := s.cleaner.DeleteAllForOwner(ctx, userID, ownerTypeNote, noteID); err != nil {
		log.Error().Err(err).Str("note_id", noteID).Msg("note: attachment cleanup failed — note deleted anyway")
	}
}

func invalid(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, msg)
}

func noteNotFound() error {
	return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "note not found")
}

// validateTitle enforces the VARCHAR(255) bound. Length is counted in runes, not
// bytes: a 200-character Hebrew title is well within the column but would fail a
// naive len() check.
func validateTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "", invalid("title is required")
	}
	if len([]rune(trimmed)) > MaxTitleLen {
		return "", invalid("title must be 255 characters or fewer")
	}
	return trimmed, nil
}

// validateContent checks that the body is a JSON object. Anything else — an
// array, a bare string, malformed bytes — is rejected rather than stored: the
// column is a document, and flattenDoc would silently yield "" for a non-object,
// making the note unsearchable with no error to explain why.
func validateContent(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) > MaxContentLen {
		return nil, invalid("content is too large")
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, invalid("content must be a valid document object")
	}
	return raw, nil
}

func (s *service) ListByProject(ctx context.Context, userID, projectID string) ([]NoteView, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, invalid("projectId is required")
	}
	// Verified before listing so a guessed project id yields 404 rather than an
	// empty list — an empty list would still confirm the project is reachable.
	if err := s.projects.VerifyProjectOwner(ctx, userID, projectID); err != nil {
		return nil, err
	}

	notes, err := s.repo.ListByProject(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}

	out := make([]NoteView, 0, len(notes))
	for _, n := range notes {
		out = append(out, toView(n))
	}
	return out, nil
}

func (s *service) Get(ctx context.Context, userID, id string) (NoteDetailView, error) {
	n, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return NoteDetailView{}, err
	}
	return toDetailView(n), nil
}

func (s *service) Create(ctx context.Context, userID string, req CreateNoteRequest) (NoteDetailView, error) {
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return NoteDetailView{}, invalid("projectId is required")
	}
	title, err := validateTitle(req.Title)
	if err != nil {
		return NoteDetailView{}, err
	}

	content := json.RawMessage(EmptyDoc)
	if len(req.Content) > 0 {
		if content, err = validateContent(req.Content); err != nil {
			return NoteDetailView{}, err
		}
	}

	// A project the caller doesn't own is reported as not-found, never forbidden.
	if err := s.projects.VerifyProjectOwner(ctx, userID, projectID); err != nil {
		return NoteDetailView{}, err
	}

	created, err := s.repo.Create(ctx, Note{
		ID:          uuid.NewString(),
		UserID:      userID,
		ProjectID:   &projectID,
		Title:       title,
		Content:     content,
		ContentText: flattenDoc(content),
	})
	if err != nil {
		return NoteDetailView{}, err
	}

	// The event carries the list shape — a client syncing its list needs the
	// excerpt, not the body it just sent.
	s.emit(userID, Event{Type: EventCreated, Payload: toView(created)})
	return toDetailView(created), nil
}

// Update is the optimistic-concurrency path. The client sends the version it
// last read; the guarded UPDATE applies only if the row is still at it. A
// 0-row result is ambiguous at the repository level — missing, not owned, or
// stale — so ownership is re-checked to tell 409 from 404.
func (s *service) Update(ctx context.Context, userID, id string, req UpdateNoteRequest) (NoteDetailView, error) {
	if req.Version == nil {
		return NoteDetailView{}, invalid("version is required")
	}
	if *req.Version < 1 {
		return NoteDetailView{}, invalid("version must be a positive integer")
	}

	// Read first so an omitted field keeps its stored value — PATCH is partial,
	// and a missing title must not blank the note.
	current, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return NoteDetailView{}, err
	}

	title := current.Title
	if req.Title != nil {
		if title, err = validateTitle(*req.Title); err != nil {
			return NoteDetailView{}, err
		}
	}

	content := current.Content
	contentText := current.ContentText
	if req.Content != nil {
		if content, err = validateContent(*req.Content); err != nil {
			return NoteDetailView{}, err
		}
		// Always re-derived, never taken from the client (NIC-1886/1889).
		contentText = flattenDoc(content)
	}

	updated, ok, err := s.repo.Update(ctx, UpdateParams{
		ID: id, UserID: userID, Version: *req.Version,
		Title: title, Content: content, ContentText: contentText,
	})
	if err != nil {
		return NoteDetailView{}, err
	}
	if !ok {
		// The read above proved the note exists and is owned, so a 0-row update
		// can only mean the version moved on between that read and this write.
		return NoteDetailView{}, apperror.New(http.StatusConflict, apperror.ErrConflict,
			"note was modified by another session")
	}

	// List shape deliberately: autosave fires every ~1–2s, so broadcasting the
	// whole document on each save would be wasteful and racy.
	s.emit(userID, Event{Type: EventUpdated, Payload: toView(updated)})
	return toDetailView(updated), nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	ok, err := s.repo.Delete(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return noteNotFound()
	}

	// Best-effort, after the row is gone: cleanup never blocks or fails a delete
	// that already committed.
	s.cleanAttachments(ctx, userID, id)
	s.emit(userID, Event{Type: EventDeleted, Payload: DeletedPayload{ID: id}})
	return nil
}
