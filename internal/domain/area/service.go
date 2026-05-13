package area

// Service defines the area business logic interface.
// Methods are added per story (E-011).
type Service interface{}

type service struct{ repo Repository }

// NewService creates a new area service.
func NewService(repo Repository) Service { return &service{repo: repo} }
