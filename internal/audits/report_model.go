package audits

import (
	"time"
)

// Report is a versioned snapshot of an audit for delivery.
type Report struct {
	ID                 string           `json:"id"`
	AuditID            string           `json:"auditId"`
	ProjectID          string           `json:"projectId"`
	Version            int              `json:"version"`
	Status             string           `json:"status"`
	TemplateVersion    string           `json:"templateVersion"`
	DependencyVersions map[string]any   `json:"dependencyVersions"`
	Sections           []ReportSection  `json:"sections"`
	Artifacts          []ReportArtifact `json:"artifacts,omitempty"`
	CreatedAt          time.Time        `json:"createdAt"`
	PublishedAt        *time.Time       `json:"publishedAt"`
}

// ReportSection is one rendered chapter with its inline data snapshot.
type ReportSection struct {
	Code    string         `json:"code"`
	Content map[string]any `json:"content"`
}

// ReportArtifact is a generated CSV (or future PDF) file on the server.
type ReportArtifact struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	SizeBytes   int64     `json:"sizeBytes"`
	SHA256      string    `json:"sha256"`
	DownloadURL string    `json:"downloadUrl"`
	CreatedAt   time.Time `json:"createdAt"`
}
