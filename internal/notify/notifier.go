package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// Alert is the payload delivered to webhook and email channels.
type Alert struct {
	ID         string `json:"alertId"`
	ProjectID  string `json:"projectId"`
	ScheduleID string `json:"scheduleId"`
	AuditID    string `json:"auditId,omitempty"`
	Kind       string `json:"kind"`
	Message    string `json:"message"`
}

// Notifier sends monitoring alerts to a webhook and/or SMTP email.
type Notifier struct {
	WebhookURL string
	SMTPHost   string
	SMTPPort   string
	SMTPUser   string
	SMTPPass   string
	SMTPFrom   string
	SMTPTo     string
	HTTP       *http.Client
}

func New(webhookURL, host, port, user, pass, from, to string) *Notifier {
	return &Notifier{
		WebhookURL: strings.TrimSpace(webhookURL),
		SMTPHost:   strings.TrimSpace(host),
		SMTPPort:   strings.TrimSpace(port),
		SMTPUser:   strings.TrimSpace(user),
		SMTPPass:   pass,
		SMTPFrom:   strings.TrimSpace(from),
		SMTPTo:     strings.TrimSpace(to),
		HTTP:       &http.Client{Timeout: 10 * time.Second},
	}
}

func (n *Notifier) Enabled() bool {
	return n != nil && (n.WebhookURL != "" || (n.SMTPHost != "" && n.SMTPFrom != "" && n.SMTPTo != ""))
}

// Send delivers the alert and returns per-channel results as JSON.
func (n *Notifier) Send(ctx context.Context, alert Alert) string {
	result := map[string]string{}
	if n == nil {
		return `{"status":"disabled"}`
	}
	if n.WebhookURL != "" {
		if err := n.sendWebhook(ctx, alert); err != nil {
			result["webhook"] = "failed: " + err.Error()
		} else {
			result["webhook"] = "sent"
		}
	}
	if n.SMTPHost != "" && n.SMTPFrom != "" && n.SMTPTo != "" {
		if err := n.sendEmail(alert); err != nil {
			result["email"] = "failed: " + err.Error()
		} else {
			result["email"] = "sent"
		}
	}
	if len(result) == 0 {
		return `{"status":"not_configured"}`
	}
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

func (n *Notifier) sendWebhook(ctx context.Context, alert Alert) error {
	body, _ := json.Marshal(alert)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := n.HTTP.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", response.StatusCode)
	}
	return nil
}

func (n *Notifier) sendEmail(alert Alert) error {
	port := n.SMTPPort
	if port == "" {
		port = "25"
	}
	address := n.SMTPHost + ":" + port
	var auth smtp.Auth
	if n.SMTPUser != "" {
		auth = smtp.PlainAuth("", n.SMTPUser, n.SMTPPass, n.SMTPHost)
	}
	subject := "[GEO monitoring] " + alert.Kind
	body := "Alert: " + alert.Kind + "\n\n" + alert.Message + "\n\nSchedule: " + alert.ScheduleID + "\nProject: " + alert.ProjectID
	if alert.AuditID != "" {
		body += "\nAudit: " + alert.AuditID
	}
	message := []byte("To: " + n.SMTPTo + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n")
	return smtp.SendMail(address, auth, n.SMTPFrom, []string{n.SMTPTo}, message)
}
