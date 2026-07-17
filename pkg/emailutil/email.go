package emailutil

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// sendTimeout bounds the entire SMTP exchange (dial + handshake + send). The
// stdlib smtp.SendMail uses no deadline, so a slow or unreachable host can hang
// the calling goroutine for minutes (the OS TCP timeout). A short, explicit
// deadline guarantees the send fails fast instead.
const sendTimeout = 10 * time.Second

const fromAddr = "noreply@nicoflow.app"

// SendPasswordReset sends a password reset email via SMTP.
// smtpDSN format: smtp://user:pass@host:port
func SendPasswordReset(to, resetURL, smtpDSN string) error {
	return send(smtpDSN, to, "Reset your Nicoflow password", buildResetEmail(resetURL))
}

// SendVerificationEmail sends an email-address verification message via SMTP.
// verifyURL should be the frontend link carrying the raw verification token.
// smtpDSN format: smtp://user:pass@host:port
func SendVerificationEmail(to, verifyURL, smtpDSN string) error {
	return send(smtpDSN, to, "Verify your Nicoflow email", buildVerificationEmail(verifyURL))
}

// SendPasswordChanged notifies the user that their account password was
// changed. Defense-in-depth: if the change wasn't them, they learn immediately
// and can recover the account. smtpDSN format: smtp://user:pass@host:port
func SendPasswordChanged(to, smtpDSN string) error {
	return send(smtpDSN, to, "Your Nicoflow password was changed", buildPasswordChangedEmail())
}

// send performs a timeout-bounded SMTP delivery. Unlike smtp.SendMail it dials
// with a deadline and sets a connection-wide deadline covering the whole
// exchange, so a hung host cannot block the caller indefinitely. It negotiates
// STARTTLS when the server advertises it (e.g. Mailtrap on :587).
func send(smtpDSN, to, subject, htmlBody string) error {
	host, auth, addr, err := parseDSN(smtpDSN)
	if err != nil {
		return fmt.Errorf("emailutil: parse DSN: %w", err)
	}

	msg := []byte(
		"From: " + fromAddr + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/html; charset=UTF-8\r\n" +
			"\r\n" +
			htmlBody,
	)

	conn, err := net.DialTimeout("tcp", addr, sendTimeout)
	if err != nil {
		return fmt.Errorf("emailutil: dial: %w", err)
	}
	// A single deadline on the raw connection bounds dial + every subsequent
	// SMTP command/read for the life of this send.
	if err := conn.SetDeadline(time.Now().Add(sendTimeout)); err != nil {
		_ = conn.Close()
		return fmt.Errorf("emailutil: set deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("emailutil: smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("emailutil: starttls: %w", err)
		}
	}

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("emailutil: auth: %w", err)
			}
		}
	}

	if err := client.Mail(fromAddr); err != nil {
		return fmt.Errorf("emailutil: mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("emailutil: rcpt to: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("emailutil: data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("emailutil: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("emailutil: close data: %w", err)
	}

	return client.Quit()
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

func buildPasswordChangedEmail() string {
	return strings.Join([]string{
		`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;padding:20px">`,
		`<h2>Your Nicoflow password was changed</h2>`,
		`<p>The password on your Nicoflow account was just changed. All other devices have been signed out.</p>`,
		`<p style="color:#6b7280;font-size:13px;margin-top:24px">If this wasn't you, reset your password immediately and contact support.</p>`,
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
