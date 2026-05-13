package billing

// Service defines the billing business logic interface.
// Methods are added per story (E-029).
type Service interface{}

type service struct{ repo Repository }

// NewService creates a new billing service.
func NewService(repo Repository) Service { return &service{repo: repo} }
