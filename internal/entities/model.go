package entities

import "time"

// Entity is one project-scoped organization, brand, product or software entity.
type Entity struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"projectId"`
	EntityType     string    `json:"entityType"`
	CanonicalName  string    `json:"canonicalName"`
	NormalizedName string    `json:"normalizedName"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// Evidence is one source quote supporting an entity or relation.
type Evidence struct {
	ID               string    `json:"id"`
	SourceType       string    `json:"sourceType"`
	SourceID         string    `json:"sourceId"`
	Quote            string    `json:"quote"`
	Locator          string    `json:"locator"`
	ExtractorVersion string    `json:"extractorVersion"`
	CreatedAt        time.Time `json:"createdAt"`
}

// Relation links two entities with a predicate.
type Relation struct {
	ID          string  `json:"id"`
	Predicate   string  `json:"predicate"`
	SubjectID   string  `json:"subjectId"`
	SubjectName string  `json:"subjectName"`
	ObjectID    string  `json:"objectId"`
	ObjectName  string  `json:"objectName"`
	Status      string  `json:"status"`
	Confidence  float64 `json:"confidence"`
}

// Detail includes aliases, evidence and relations for the entity page.
type Detail struct {
	Entity
	Aliases   []string   `json:"aliases"`
	Evidence  []Evidence `json:"evidence"`
	Relations []Relation `json:"relations"`
}

// MergeEvent records one reversible merge.
type MergeEvent struct {
	ID             string    `json:"id"`
	SourceEntityID string    `json:"sourceEntityId"`
	TargetEntityID string    `json:"targetEntityId"`
	Reason         string    `json:"reason"`
	Status         string    `json:"status"`
	OccurredAt     time.Time `json:"occurredAt"`
}

// MergeInput identifies the source and target of a merge.
type MergeInput struct {
	SourceEntityID string `json:"sourceEntityId"`
	TargetEntityID string `json:"targetEntityId"`
	Reason         string `json:"reason"`
}

// RelationCandidate is a relation parsed from structured data.
type RelationCandidate struct {
	Predicate  string
	TargetName string
	TargetType string
}

// Candidate is one entity parsed from JSON-LD before persistence.
type Candidate struct {
	Name      string
	Type      string
	Aliases   []string
	Locator   string
	Relations []RelationCandidate
}
