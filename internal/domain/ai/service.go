package ai

// Service defines the AI assistant business logic interface.
// Methods are added per story (E-026).
type Service interface{}

type service struct{ repo Repository }

// NewService creates a new AI service.
func NewService(repo Repository) Service { return &service{repo: repo} }
