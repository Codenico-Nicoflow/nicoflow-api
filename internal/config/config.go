package config

import (
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
)

type Config struct {
	DatabaseURL        string
	JWTSecret          string
	JWTExpiry          time.Duration
	RefreshTokenExpiry time.Duration
	SMTPDsn            string
	AppBaseURL         string
	// Gate login on a verified email. Default false so dev (no SMTP) isn't locked out.
	RequireEmailVerification bool
	// CookieCrossSite makes the refresh cookie SameSite=None (instead of Strict) so it is
	// sent on cross-site requests — required when the frontend and API are on different
	// registrable domains (e.g. *.vercel.app calling *.onrender.com). SameSite=None demands
	// Secure, so this is only honoured in secure environments. Default false (same-site, Strict).
	CookieCrossSite bool
	LSWebhookSecret string
	// CronSecret guards the internal job endpoints (POST /internal/jobs/*). Callers
	// must send it as X-Internal-Token. Unset ⇒ the endpoints return 503 (disabled).
	CronSecret string
	// Storage* configure the S3-compatible object store for file attachments.
	// The backend is Cloudflare R2 (staging/prod) or MinIO (local) — both speak
	// the S3 API — so the vars are vendor-neutral, not AWS-specific.
	StorageAccessKeyID string
	StorageSecretKey   string
	// StorageRegion is "auto" for R2, a real region for AWS S3, anything for MinIO.
	StorageRegion string
	// StorageEndpoint overrides the S3 endpoint (R2 or MinIO URL). Empty ⇒ real
	// AWS S3. When set, the client uses path-style addressing.
	StorageEndpoint   string
	StorageBucket     string
	CORSOrigins       string
	TrustedProxyCIDRs string
	// RateLimitBypassToken lets the E2E suite opt out of rate limiting: a request
	// carrying X-E2E-Bypass equal to this value skips the IP/user limiters. Only
	// set on staging (never prod). Unset ⇒ no bypass, limits always apply.
	RateLimitBypassToken string
	AppEnv               string
	Port                 string
	LogLevel             string
	// SkipMigrations disables the run-on-boot migration step (default false).
	SkipMigrations bool
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	// VAPID keys for Web Push (E-025 / NIC-1580). Base64url-encoded per RFC 8292.
	// All three unset ⇒ web push is a no-op (safe local/dev, mirrors SMTP_DSN).
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// AnthropicAPIKey enables the AI assistant (E-026). Unset ⇒ /v1/ai/* returns
	// 503 AI_UNAVAILABLE (kill switch), never a silent no-op — mirrors storage.
	AnthropicAPIKey string
	// AIModel is the Claude model ID for the assistant. Never hardcode a model ID
	// at the call site; it always comes from here (default claude-haiku-4-5).
	AIModel string
	// Google* configure the read-only Google Calendar overlay (E-052 / NIC-1838).
	// Any of the four unset ⇒ the whole integration is a silent no-op and the
	// calendar renders tasks only (mirrors VAPID/storage). GoogleRedirectURL must
	// exact-match a URI registered on the OAuth client.
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	// GoogleTokenEncKey is the base64-encoded 32-byte AES-256-GCM key encrypting
	// the stored refresh token at rest.
	GoogleTokenEncKey string
}

// GoogleEnabled reports whether the Google Calendar integration is configured.
//
// All four values are required together: credentials without the encryption key
// would mean storing a live refresh token in plaintext, and the key without
// credentials encrypts nothing. Treating them as one unit makes a half-configured
// environment behave as "off" rather than as a security hole.
func (c Config) GoogleEnabled() bool {
	return c.GoogleClientID != "" &&
		c.GoogleClientSecret != "" &&
		c.GoogleRedirectURL != "" &&
		c.GoogleTokenEncKey != ""
}

// defaultAIModel is the Claude model used when AI_MODEL is unset.
const defaultAIModel = "claude-haiku-4-5"

func Load() Config {
	databaseURL := os.Getenv("DATABASE_URL")
	jwtSecret := os.Getenv("JWT_SECRET")
	port := os.Getenv("PORT")

	if databaseURL == "" {
		log.Fatal().Msg("required env var DATABASE_URL is not set")
	}
	if jwtSecret == "" {
		log.Fatal().Msg("required env var JWT_SECRET is not set")
	}
	if len(jwtSecret) < 32 {
		log.Fatal().Msg("JWT_SECRET must be at least 32 bytes — generate one with: openssl rand -base64 48")
	}
	if port == "" {
		log.Fatal().Msg("required env var PORT is not set")
	}

	jwtExpiry := parseDuration(os.Getenv("JWT_EXPIRY"), 15*time.Minute)
	refreshExpiry := parseDuration(os.Getenv("REFRESH_TOKEN_EXPIRY"), 7*24*time.Hour)

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	aiModel := os.Getenv("AI_MODEL")
	if aiModel == "" {
		aiModel = defaultAIModel
	}

	return Config{
		DatabaseURL:        databaseURL,
		JWTSecret:          jwtSecret,
		JWTExpiry:          jwtExpiry,
		RefreshTokenExpiry: refreshExpiry,
		SMTPDsn:            os.Getenv("SMTP_DSN"),
		AppBaseURL:         os.Getenv("APP_BASE_URL"),

		RequireEmailVerification: parseBool(os.Getenv("REQUIRE_EMAIL_VERIFICATION"), false),
		CookieCrossSite:          parseBool(os.Getenv("COOKIE_CROSS_SITE"), false),
		LSWebhookSecret:          os.Getenv("LS_WEBHOOK_SECRET"),
		CronSecret:               os.Getenv("CRON_SECRET"),
		StorageAccessKeyID:       os.Getenv("STORAGE_ACCESS_KEY_ID"),
		StorageSecretKey:         os.Getenv("STORAGE_SECRET_ACCESS_KEY"),
		StorageRegion:            os.Getenv("STORAGE_REGION"),
		StorageEndpoint:          os.Getenv("STORAGE_ENDPOINT"),
		StorageBucket:            os.Getenv("STORAGE_BUCKET"),
		CORSOrigins:              os.Getenv("CORS_ORIGINS"),
		TrustedProxyCIDRs:        os.Getenv("TRUSTED_PROXY"),
		RateLimitBypassToken:     os.Getenv("RATE_LIMIT_BYPASS_TOKEN"),
		AppEnv:                   os.Getenv("APP_ENV"),
		Port:                     port,
		LogLevel:                 logLevel,
		SkipMigrations:           parseBool(os.Getenv("SKIP_MIGRATIONS"), false),
		MinIOEndpoint:            os.Getenv("MINIO_ENDPOINT"),
		MinIOAccessKey:           os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:           os.Getenv("MINIO_SECRET_KEY"),
		VAPIDPublicKey:           os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey:          os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:             os.Getenv("VAPID_SUBJECT"),
		AnthropicAPIKey:          os.Getenv("ANTHROPIC_API_KEY"),
		AIModel:                  aiModel,
		GoogleClientID:           os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:       os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:        os.Getenv("GOOGLE_REDIRECT_URL"),
		GoogleTokenEncKey:        os.Getenv("GOOGLE_TOKEN_ENC_KEY"),
	}
}

// String returns a safe representation that redacts secrets.
// Prevents JWTSecret, LSWebhookSecret and the Google client secret / token
// encryption key leaking into debug logs.
func (c Config) String() string {
	return "config{env=" + c.AppEnv + " port=" + c.Port +
		" jwt_secret=[REDACTED] webhook_secret=[REDACTED]" +
		" google_client_secret=[REDACTED] google_token_enc_key=[REDACTED]}"
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}

func parseBool(s string, fallback bool) bool {
	if s == "" {
		return fallback
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return fallback
	}
	return b
}
