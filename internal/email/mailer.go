package email

import (
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strconv"
)

type Mailer struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

func NewMailer() *Mailer {
	portStr := os.Getenv("SMTP_PORT")
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 587
	}
	return &Mailer{
		Host: os.Getenv("SMTP_HOST"),
		Port: port,
		User: os.Getenv("SMTP_USER"),
		Pass: os.Getenv("SMTP_PASS"),
		From: os.Getenv("SMTP_FROM"),
	}
}

// SendInviteEmail dispatches an invitation email or logs it if SMTP is unconfigured.
func (m *Mailer) SendInviteEmail(toEmail, orgName, role, inviteURL string) error {
	subject := fmt.Sprintf("You're invited to join %s on Mini Tracker (Beta)", orgName)

	body := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <style>
    body { font-family: 'Segoe UI', system-ui, -apple-system, sans-serif; background-color: #0f172a; color: #f8fafc; margin: 0; padding: 40px; }
    .container { max-width: 560px; margin: 0 auto; background: #1e293b; border: 1px solid #334155; border-radius: 12px; padding: 32px; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.5); }
    .logo { font-size: 24px; font-weight: 700; color: #38bdf8; letter-spacing: -0.5px; margin-bottom: 24px; display: inline-block; }
    h1 { font-size: 20px; color: #f8fafc; margin-top: 0; margin-bottom: 12px; }
    p { font-size: 15px; color: #94a3b8; line-height: 1.6; margin-bottom: 24px; }
    .badge { display: inline-block; padding: 4px 10px; background: rgba(56, 189, 248, 0.15); color: #38bdf8; border: 1px solid rgba(56, 189, 248, 0.3); border-radius: 9999px; font-size: 13px; font-weight: 600; text-transform: uppercase; }
    .btn { display: inline-block; background: #2563eb; color: #ffffff; text-decoration: none; padding: 12px 28px; border-radius: 8px; font-weight: 600; font-size: 15px; text-align: center; }
    .footer { margin-top: 32px; font-size: 12px; color: #64748b; border-top: 1px solid #334155; padding-top: 16px; }
  </style>
</head>
<body>
  <div class="container">
    <div class="logo">⚡ Mini Tracker Corporate Beta</div>
    <h1>Join %s</h1>
    <p>You have been invited to join <strong>%s</strong> as a <span class="badge">%s</span> on Mini Tracker Corporate Beta.</p>
    <p>Click below to complete your setup and join your team:</p>
    <div style="text-align: center; margin: 32px 0;">
      <a href="%s" class="btn">Accept Invitation & Join Team</a>
    </div>
    <p style="font-size: 13px; color: #64748b;">Or copy this link into your browser:<br><a href="%s" style="color: #38bdf8;">%s</a></p>
    <div class="footer">
      This invitation expires in 48 hours. If you did not expect this invite, please ignore this email.
    </div>
  </div>
</body>
</html>`, orgName, orgName, role, inviteURL, inviteURL, inviteURL)

	if m.Host == "" || m.From == "" {
		log.Printf("--------------------------------------------------")
		log.Printf("[MAILER SIMULATION] SMTP not configured. Invite URL:")
		log.Printf("[INVITE TO %s]: %s", toEmail, inviteURL)
		log.Printf("--------------------------------------------------")
		return nil
	}

	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\n\n"
	msg := []byte(fmt.Sprintf("To: %s\nFrom: %s\nSubject: %s\n%s%s", toEmail, m.From, subject, mime, body))

	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)

	if err := smtp.SendMail(addr, auth, m.From, []string{toEmail}, msg); err != nil {
		log.Printf("[MAILER ERROR] Failed to send email to %s: %v. Fallback link: %s", toEmail, err, inviteURL)
		return nil // Graceful fallback so API succeeds
	}

	log.Printf("[MAILER SUCCESS] Invitation sent to %s", toEmail)
	return nil
}
