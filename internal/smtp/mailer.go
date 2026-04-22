package smtp

import (
	"bytes"
	"crypto/tls"
	_ "embed"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net/smtp"
	"time"
)

// Config holds SMTP configuration.
type Config struct {
	Host     string `json:"SMTP_HOST"`
	Port     int    `json:"SMTP_PORT"`
	Username string `json:"SMTP_USERNAME"`
	Password string `json:"SMTP_PASSWORD"`
	From     string `json:"SMTP_FROM"`
}

// IsConfigured returns true if SMTP is properly configured.
func (c *Config) IsConfigured() bool {
	return c.Host != "" && c.Port > 0 && c.Username != "" && c.Password != ""
}

// Mailer sends emails via SMTP.
type Mailer struct {
	cfg Config
}

// New creates a new SMTP mailer.
func New(cfg Config) *Mailer {
	return &Mailer{cfg: cfg}
}

// UpdateConfig updates the SMTP configuration.
func (m *Mailer) UpdateConfig(cfg Config) {
	m.cfg = cfg
}

// emailHeader returns the branded Moca Consult email wrapper.
func emailHeader(baseURL string) string {
	return `<!DOCTYPE html>
<html lang="fr">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta name="color-scheme" content="light dark">
  <meta name="supported-color-schemes" content="light dark">
</head>
<body style="background-color:#0a0a0f;margin:0;padding:0;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen-Sans,Ubuntu,Cantarell,'Helvetica Neue',sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" role="presentation" style="background-color:#0a0a0f;">
    <tr>
      <td align="center" style="padding: 60px 20px;">
        <table width="480" cellpadding="0" cellspacing="0" role="presentation" style="margin:0 auto;">
          <!-- Logo -->
          <tr>
            <td align="center" style="padding-bottom:32px;">
              <img src="cid:logo_img" alt="Moca Consult" style="height:72px;display:block;">
            </td>
          </tr>
          <!-- Dark Card -->
          <tr>
            <td style="background-color:#1a1a2e;border:1px solid #2a2a40;border-radius:16px;padding:48px 40px;">`
}

func emailFooter() string {
	return `
            </td>
          </tr>
        </table>
        <!-- Footer -->
        <table width="480" cellpadding="0" cellspacing="0" role="presentation" style="margin:0 auto;">
          <tr>
            <td align="center" style="padding-top:32px;color:#6b6b80;font-size:12px;line-height:1.5;">
              <p style="margin:0;color:#6b6b80;">Cet email est généré automatiquement par Fiber Tracker.</p>
              <p style="margin:4px 0 0;color:#6b6b80;">&copy; 2026 Moca Consult. Tous droits réservés.</p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`
}

// SendResetEmail sends a branded password reset email.
func (m *Mailer) SendResetEmail(toEmail, baseURL, resetLink string) error {
	if !m.cfg.IsConfigured() {
		return fmt.Errorf("SMTP not configured")
	}

	subject := "Moca Consult — Réinitialisation de mot de passe"
	body := emailHeader(baseURL) + `
              <h1 style="color:#f0f0f5;font-size:26px;font-weight:700;letter-spacing:-0.5px;margin:0 0 16px;text-align:center;">Réinitialisation du mot de passe</h1>
              <p style="color:#a0a0b8;font-size:15px;line-height:1.6;margin:0 0 32px;text-align:center;">
                Nous avons reçu une demande de réinitialisation de mot de passe pour votre compte. Cliquez sur le bouton ci-dessous pour configurer un nouveau mot de passe.
              </p>
              
              <table width="100%" cellpadding="0" cellspacing="0" role="presentation" style="margin-bottom:32px;">
                <tr>
                  <td align="center">
                    <a href="` + resetLink + `" style="background-color:#7c3aed;border-radius:8px;color:#ffffff;display:inline-block;font-size:15px;font-weight:600;padding:14px 32px;text-decoration:none;text-align:center;">Réinitialiser le mot de passe</a>
                  </td>
                </tr>
              </table>

              <div style="background-color:#252540;border:1px solid #35355a;border-radius:8px;padding:20px;">
                <p style="color:#c4b5fd;font-size:13px;line-height:1.6;margin:0;">
                  <strong style="color:#ddd6fe;">Vous n'êtes pas à l'origine de cette demande ?</strong><br>
                  Si vous n'avez pas demandé à réinitialiser votre mot de passe, vous pouvez ignorer cet email en toute sécurité.
                </p>
              </div>` + emailFooter()

	return m.sendHTML(toEmail, subject, body)
}

// SendTestEmail sends a branded test email to verify SMTP configuration.
func (m *Mailer) SendTestEmail(toEmail, baseURL string) error {
	if !m.cfg.IsConfigured() {
		return fmt.Errorf("SMTP not configured")
	}
	if toEmail == "" {
		toEmail = m.cfg.From
	}

	subject := "Moca Consult — ✅ Test SMTP réussi"
	body := emailHeader(baseURL) + `
              <h1 style="color:#f0f0f5;font-size:26px;font-weight:700;letter-spacing:-0.5px;margin:0 0 16px;text-align:center;">Test SMTP Réussi</h1>
              <p style="color:#a0a0b8;font-size:15px;line-height:1.6;margin:0 0 36px;text-align:center;">
                Félicitations ! La configuration SMTP de votre tableau de bord <strong style="color:#f0f0f5;">Fiber Tracker</strong> fonctionne parfaitement.
              </p>

              <div style="background-color:#252540;border:1px solid #35355a;border-radius:8px;padding:24px;">
                <h2 style="color:#a78bfa;font-size:12px;font-weight:700;margin:0 0 16px;text-transform:uppercase;letter-spacing:1px;">Services Actifs</h2>
                
                <table width="100%" cellpadding="0" cellspacing="0" role="presentation">
                  <tr>
                    <td style="color:#a78bfa;font-weight:bold;padding-right:12px;vertical-align:top;width:16px;">&#10003;</td>
                    <td style="color:#d4d4e0;font-size:14px;line-height:1.6;padding-bottom:12px;">Envoi de notifications système</td>
                  </tr>
                  <tr>
                    <td style="color:#a78bfa;font-weight:bold;padding-right:12px;vertical-align:top;width:16px;">&#10003;</td>
                    <td style="color:#d4d4e0;font-size:14px;line-height:1.6;padding-bottom:12px;">Réinitialisation des mots de passe</td>
                  </tr>
                  <tr>
                    <td style="color:#a78bfa;font-weight:bold;padding-right:12px;vertical-align:top;width:16px;">&#10003;</td>
                    <td style="color:#d4d4e0;font-size:14px;line-height:1.6;">Rapports analytiques journaliers</td>
                  </tr>
                </table>
              </div>` + emailFooter()

	return m.sendHTML(toEmail, subject, body)
}



//go:embed logo-dark.png
var logoData []byte

// sendHTML sends an HTML email with an embedded logo.
func (m *Mailer) sendHTML(toEmail, subject, htmlBody string) error {
	boundary := "mocaconsult-boundary-" + fmt.Sprintf("%x", time.Now().UnixNano())

	// Start multipart/related
	encodedSubject := mime.BEncoding.Encode("utf-8", subject)

	var body bytes.Buffer
	body.WriteString(fmt.Sprintf("From: %s\r\n", m.cfg.From))
	body.WriteString(fmt.Sprintf("To: %s\r\n", toEmail))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", encodedSubject))
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString(fmt.Sprintf("Content-Type: multipart/related; boundary=\"%s\"\r\n\r\n", boundary))

	// HTML Part
	body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	body.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	body.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	
	// Base64 encode HTML to completely avoid any 7-bit/8-bit or CRLF truncation issues in strict SMTP servers
	b64HTML := base64.StdEncoding.EncodeToString([]byte(htmlBody))
	for i := 0; i < len(b64HTML); i += 76 {
		end := i + 76
		if end > len(b64HTML) {
			end = len(b64HTML)
		}
		body.WriteString(b64HTML[i:end] + "\r\n")
	}
	body.WriteString("\r\n")

	// Logo Part (CID)
	if len(logoData) > 0 {
		body.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		body.WriteString("Content-Type: image/png\r\n")
		body.WriteString("Content-ID: <logo_img>\r\n")
		body.WriteString("Content-Disposition: inline\r\n")
		body.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")

		b64 := base64.StdEncoding.EncodeToString(logoData)
		for i := 0; i < len(b64); i += 76 {
			end := i + 76
			if end > len(b64) {
				end = len(b64)
			}
			body.WriteString(b64[i:end] + "\r\n")
		}
		body.WriteString("\r\n")
	}

	body.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)

	var err error
	if m.cfg.Port == 465 {
		// SMTPS (Implicit TLS)
		tlsconfig := &tls.Config{ServerName: m.cfg.Host}
		conn, dialErr := tls.Dial("tcp", addr, tlsconfig)
		if dialErr != nil {
			log.Printf("[smtp] TLS Dial error to %s: %v", toEmail, dialErr)
			return fmt.Errorf("tls.Dial: %w", dialErr)
		}
		defer conn.Close()

		c, clientErr := smtp.NewClient(conn, m.cfg.Host)
		if clientErr != nil {
			return fmt.Errorf("smtp.NewClient: %w", clientErr)
		}
		defer c.Close()

		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("c.Auth: %w", err)
		}
		if err = c.Mail(m.cfg.From); err != nil {
			return fmt.Errorf("c.Mail: %w", err)
		}
		if err = c.Rcpt(toEmail); err != nil {
			return fmt.Errorf("c.Rcpt: %w", err)
		}
		w, dataErr := c.Data()
		if dataErr != nil {
			return fmt.Errorf("c.Data: %w", dataErr)
		}
		_, err = w.Write(body.Bytes())
		if err != nil {
			return fmt.Errorf("w.Write: %w", err)
		}
		err = w.Close()
		if err != nil {
			return fmt.Errorf("w.Close: %w", err)
		}
		c.Quit()
	} else {
		// STARTTLS for port 587/25
		err = smtp.SendMail(addr, auth, m.cfg.From, []string{toEmail}, body.Bytes())
	}

	if err != nil {
		log.Printf("[smtp] Send error to %s: %v", toEmail, err)
		return fmt.Errorf("send email: %w", err)
	}

	log.Printf("[smtp] Email sent to %s (subject: %s)", toEmail, subject)
	return nil
}
