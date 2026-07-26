package ai

// Service defines the AI assistant business logic interface.
// Methods are added per story (E-026).
type Service interface{}

type service struct {
	repo   Repository
	client Client
	model  string
}

// NewService creates a new AI service. client is the streaming provider seam
// (nil-safe: a disabled client is fine); model is the config-resolved Claude
// model ID — never hardcoded at the call site.
func NewService(repo Repository, client Client, model string) Service {
	return &service{repo: repo, client: client, model: model}
}
