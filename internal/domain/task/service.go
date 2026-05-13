package task

// Service defines the task business logic interface.
// Methods are added per story (E-013).
type Service interface{}

type service struct{ repo Repository }

// NewService creates a new task service.
func NewService(repo Repository) Service { return &service{repo: repo} }
