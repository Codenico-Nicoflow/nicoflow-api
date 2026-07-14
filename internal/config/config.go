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
	CronSecret        string
	AWSAccessKeyID    string
	AWSSecretKey      string
	S3Bucket          string
	CORSOrigins       string
	TrustedProxyCIDRs string
	AppEnv            string
	Port              string
	LogLevel          string
	// SkipMigrations disables the run-on-boot migration step (default false).
	SkipMigrations bool
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
}

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
		AWSAccessKeyID:           os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:             os.Getenv("AWS_SECRET_ACCESS_KEY"),
		S3Bucket:                 os.Getenv("S3_BUCKET_NAME"),
		CORSOrigins:              os.Getenv("CORS_ORIGINS"),
		TrustedProxyCIDRs:        os.Getenv("TRUSTED_PROXY"),
		AppEnv:                   os.Getenv("APP_ENV"),
		Port:                     port,
		LogLevel:                 logLevel,
		SkipMigrations:           parseBool(os.Getenv("SKIP_MIGRATIONS"), false),
		MinIOEndpoint:            os.Getenv("MINIO_ENDPOINT"),
		MinIOAccessKey:           os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:           os.Getenv("MINIO_SECRET_KEY"),
	}
}

// String returns a safe representation that redacts secrets.
// Prevents JWTSecret and LSWebhookSecret leaking into debug logs.
func (c Config) String() string {
	return "config{env=" + c.AppEnv + " port=" + c.Port + " jwt_secret=[REDACTED] webhook_secret=[REDACTED]}"
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
