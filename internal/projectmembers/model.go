package projectmembers

import "time"

// Member is a project-scoped grant for a tenant user.
type Member struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	Email        string    `json:"email"`
	RoleOverride *string   `json:"roleOverride"`
	CreatedAt    time.Time `json:"createdAt"`
}
