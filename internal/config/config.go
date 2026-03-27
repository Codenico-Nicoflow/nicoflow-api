package config

import (
	"os"
)

type Config struct {
	DBUrl             string
	JWTSecret         string
	LSWebhookSecret   string
	AWSAccessKeyID    string
	AWSSecretKey      string
	S3Bucket          string
	CORSOrigins       string
	AppEnv            string
	Port              string
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return Config{
		DBUrl:           os.Getenv("DB_URL"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		LSWebhookSecret: os.Getenv("LS_WEBHOOK_SECRET"),
		AWSAccessKeyID:  os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:    os.Getenv("AWS_SECRET_ACCESS_KEY"),
		S3Bucket:        os.Getenv("S3_BUCKET_NAME"),
		CORSOrigins:     os.Getenv("CORS_ORIGINS"),
		AppEnv:          os.Getenv("APP_ENV"),
		Port:            port,
	}
}
