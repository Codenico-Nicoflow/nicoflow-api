package emailutil

import (
	"html"
	"strconv"
	"strings"
)

// DigestTask is the minimal task shape the due-digest email renders.
type DigestTask struct {
	Title string
}

// SendDueDigest sends a Pro user one batched email listing their tasks due soon.
// An empty smtpDSN is a safe no-op (matches the auth flow's SMTP gating) — dev
// environments without SMTP configured simply skip delivery. An empty task list
// is also a no-op (nothing to summarise).
func SendDueDigest(to string, tasks []DigestTask, smtpDSN string) error {
	if smtpDSN == "" || len(tasks) == 0 {
		return nil
	}
	subject, body := buildDueDigest(tasks)
	return send(smtpDSN, to, subject, body)
}

// buildDueDigest renders the digest subject and HTML body. Task titles are
// HTML-escaped — they are user-controlled and land in markup.
func buildDueDigest(tasks []DigestTask) (subject, htmlBody string) {
	n := len(tasks)
	noun := "task"
	if n != 1 {
		noun = "tasks"
	}
	subject = "You have " + strconv.Itoa(n) + " " + noun + " due soon"

	var items strings.Builder
	for _, t := range tasks {
		items.WriteString(`<li style="margin:6px 0">` + html.EscapeString(t.Title) + `</li>`)
	}

	htmlBody = strings.Join([]string{
		`<!DOCTYPE html><html><body style="font-family:sans-serif;max-width:600px;margin:0 auto;padding:20px">`,
		`<h2>Your Nicoflow tasks due soon</h2>`,
		`<p>These tasks are coming up:</p>`,
		`<ul style="padding-left:20px">` + items.String() + `</ul>`,
		`<p style="color:#6b7280;font-size:13px;margin-top:24px">You're receiving this because email digests are on. Turn them off anytime in your notification settings.</p>`,
		`</body></html>`,
	}, "\n")
	return subject, htmlBody
}
