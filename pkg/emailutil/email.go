package emailutil

import (
	"fmt"
	"net/smtp"
	"net/url"
	"strings"
)

// SendPasswordReset sends a password reset email via SMTP.
// smtpDSN format: smtp://user:pass@host:port
func SendPasswordReset(to, resetURL, smtpDSN string) error {
	_, auth, addr, err := parseDSN(smtpDSN)
	if err != nil {
		return fmt.Errorf("emailutil: parse DSN: %w", err)
	}

	from := "noreply@nicoflow.app"
	subject := "Reset your Nicoflow password"
	body := buildResetEmail(resetURL)

	msg := []byte(
		"From: " + from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/html; charset=UTF-8\r\n" +
			"\r\n" +
			body,
	)

	if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
		return fmt.Errorf("emailutil: send: %w", err)
	}
	return nil
}

// SendVerificationEmail sends an email-address verification message via SMTP.
// verifyURL should be the frontend link carrying the raw verification token.
// smtpDSN format: smtp://user:pass@host:port
func SendVerificationEmail(to, verifyURL, smtpDSN string) error {
	_, auth, addr, err := parseDSN(smtpDSN)
	if err != nil {
		return fmt.Errorf("emailutil: parse DSN: %w", err)
	}

	from := "noreply@nicoflow.app"
	subject := "Verify your Nicoflow email"
	body := buildVerificationEmail(verifyURL)

	msg := []byte(
		"From: " + from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/html; charset=UTF-8\r\n" +
			"\r\n" +
			body,
	)

	if err := smtp.SendMail(addr, auth, from, []string{to}, msg); err != nil {
		return fmt.Errorf("emailutil: send: %w", err)
	}
	return nil
}

func parseDSN(dsn string) (smtpHost string, auth smtp.Auth, addr string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", nil, "", fmt.Errorf("invalid DSN: %w", err)
	}
	smtpHost = u.Hostname()
	port := u.Port()
	if port == "" {
		port = "587"
	}
	addr = smtpHost + ":" + port

	user := u.User.Username()
	pass, _ := u.User.Password()
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, smtpHost)
	}
	return smtpHost, auth, addr, nil
}

func buildVerificationEmail(verifyURL string) string {
	return strings.Join([]string{
		`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;padding:20px">`,
		`<h2>Verify your Nicoflow email</h2>`,
		`<p>Click the button below to confirm your email address. This link expires in <strong>24 hours</strong>.</p>`,
		`<a href="` + verifyURL + `" style="display:inline-block;padding:12px 24px;background:#6366f1;color:#fff;text-decoration:none;border-radius:6px;font-weight:bold">Verify email</a>`,
		`<p style="color:#6b7280;font-size:13px;margin-top:24px">If you didn't create a Nicoflow account, you can safely ignore this email.</p>`,
		`</body></html>`,
	}, "\n")
}

func buildResetEmail(resetURL string) string {
	return strings.Join([]string{
		`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;padding:20px">`,
		`<h2>Reset your Nicoflow password</h2>`,
		`<p>Click the button below to set a new password. This link expires in <strong>1 hour</strong>.</p>`,
		`<a href="` + resetURL + `" style="display:inline-block;padding:12px 24px;background:#6366f1;color:#fff;text-decoration:none;border-radius:6px;font-weight:bold">Reset password</a>`,
		`<p style="color:#6b7280;font-size:13px;margin-top:24px">If you didn't request this, you can safely ignore this email.</p>`,
		`</body></html>`,
	}, "\n")
}
