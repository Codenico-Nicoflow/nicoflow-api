package bucket

// Service defines the bucket (inbox) business logic interface.
// Methods are added per story (E-015).
type Service interface{}

type service struct{ repo Repository }

// NewService creates a new bucket service.
func NewService(repo Repository) Service { return &service{repo: repo} }
