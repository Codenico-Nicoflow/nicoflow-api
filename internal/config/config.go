package config

import (
	"os"
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
	LSWebhookSecret    string
	AWSAccessKeyID     string
	AWSSecretKey       string
	S3Bucket           string
	CORSOrigins        string
	AppEnv             string
	Port               string
	LogLevel           string
	MinIOEndpoint      string
	MinIOAccessKey     string
	MinIOSecretKey     string
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
		LSWebhookSecret:    os.Getenv("LS_WEBHOOK_SECRET"),
		AWSAccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:       os.Getenv("AWS_SECRET_ACCESS_KEY"),
		S3Bucket:           os.Getenv("S3_BUCKET_NAME"),
		CORSOrigins:        os.Getenv("CORS_ORIGINS"),
		AppEnv:             os.Getenv("APP_ENV"),
		Port:               port,
		LogLevel:           logLevel,
		MinIOEndpoint:      os.Getenv("MINIO_ENDPOINT"),
		MinIOAccessKey:     os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:     os.Getenv("MINIO_SECRET_KEY"),
	}
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
