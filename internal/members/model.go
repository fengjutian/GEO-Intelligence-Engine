package members

import "time"

// Member is a tenant user with an assigned role.
type Member struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Email     string    `json:"email"`
	Subject   string    `json:"subject"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// Invitation is a pending tenant invitation. Token is returned only at creation.
type Invitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expiresAt"`
	Token     string    `json:"token,omitempty"`
}

// InviteInput creates an invitation for an email address.
type InviteInput struct {
	Email          string `json:"email"`
	Role           string `json:"role"`
	ExpiresInHours int    `json:"expiresInHours"`
}

// UpdateInput changes a membership role or status.
type UpdateInput struct {
	Role   string `json:"role"`
	Status string `json:"status"`
}
