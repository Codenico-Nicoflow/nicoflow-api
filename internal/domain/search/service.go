package search

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

const (
	minTermLen   = 2
	maxTermLen   = 100
	defaultLimit = 10
	maxLimit     = 50
)

// Service defines the search business logic interface.
type Service interface {
	Search(ctx context.Context, userID string, q Query) (Response, error)
}

type service struct {
	repo Repository
}

// NewService returns the search Service.
func NewService(repo Repository) Service { return &service{repo: repo} }

// validTypes is the closed set of searchable result groups.
var validTypes = map[string]struct{}{
	TypeTask:    {},
	TypeProject: {},
	TypeArea:    {},
}

// Validate normalises and checks a raw query. It trims the term, enforces the
// 2–100 char bound, validates requested types against the closed set, and
// clamps the limit into [1, maxLimit] (default 10). Any violation returns a
// typed INVALID_INPUT AppError so no unbounded scan path is reachable.
func Validate(rawTerm string, rawTypes []string, limit int) (Query, error) {
	term := strings.TrimSpace(rawTerm)
	n := utf8.RuneCountInString(term)
	if n < minTermLen || n > maxTermLen {
		return Query{}, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput,
			"q must be between 2 and 100 characters")
	}

	types := make([]string, 0, len(rawTypes))
	for _, t := range rawTypes {
		t = strings.TrimSpace(strings.ToLower(t))
		if t == "" {
			continue
		}
		if _, ok := validTypes[t]; !ok {
			return Query{}, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput,
				"types must be a comma-separated subset of task,project,area")
		}
		types = append(types, t)
	}

	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	return Query{Term: term, Types: types, Limit: limit}, nil
}

func (q Query) wants(group string) bool {
	if len(q.Types) == 0 {
		return true // omitted → all groups
	}
	for _, t := range q.Types {
		if t == group {
			return true
		}
	}
	return false
}

// Search runs the requested per-type queries and returns grouped results.
// Only the requested groups are populated; omitted `types` searches all three.
func (s *service) Search(ctx context.Context, userID string, q Query) (Response, error) {
	resp := Response{
		Tasks:    []TaskResult{},
		Projects: []ProjectResult{},
		Areas:    []AreaResult{},
	}

	if q.wants(TypeTask) {
		tasks, err := s.repo.SearchTasks(ctx, userID, q.Term, q.Limit)
		if err != nil {
			return Response{}, err
		}
		if tasks != nil {
			resp.Tasks = tasks
		}
	}
	if q.wants(TypeProject) {
		projects, err := s.repo.SearchProjects(ctx, userID, q.Term, q.Limit)
		if err != nil {
			return Response{}, err
		}
		if projects != nil {
			resp.Projects = projects
		}
	}
	if q.wants(TypeArea) {
		areas, err := s.repo.SearchAreas(ctx, userID, q.Term, q.Limit)
		if err != nil {
			return Response{}, err
		}
		if areas != nil {
			resp.Areas = areas
		}
	}

	return resp, nil
}
