package config

import (
	"os"

	"github.com/rs/zerolog/log"
)

type Config struct {
	DatabaseURL     string
	JWTSecret       string
	LSWebhookSecret string
	AWSAccessKeyID  string
	AWSSecretKey    string
	S3Bucket        string
	CORSOrigins     string
	AppEnv          string
	Port            string
	MinIOEndpoint   string
	MinIOAccessKey  string
	MinIOSecretKey  string
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

	return Config{
		DatabaseURL:     databaseURL,
		JWTSecret:       jwtSecret,
		LSWebhookSecret: os.Getenv("LS_WEBHOOK_SECRET"),
		AWSAccessKeyID:  os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:    os.Getenv("AWS_SECRET_ACCESS_KEY"),
		S3Bucket:        os.Getenv("S3_BUCKET_NAME"),
		CORSOrigins:     os.Getenv("CORS_ORIGINS"),
		AppEnv:          os.Getenv("APP_ENV"),
		Port:            port,
		MinIOEndpoint:   os.Getenv("MINIO_ENDPOINT"),
		MinIOAccessKey:  os.Getenv("MINIO_ACCESS_KEY"),
		MinIOSecretKey:  os.Getenv("MINIO_SECRET_KEY"),
	}
}
