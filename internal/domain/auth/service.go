package auth

// Service defines the auth and user management business logic interface.
// Methods are added per story (E-009).
type Service interface{}

type service struct{ repo Repository }

// NewService creates a new auth service.
func NewService(repo Repository) Service { return &service{repo: repo} }
