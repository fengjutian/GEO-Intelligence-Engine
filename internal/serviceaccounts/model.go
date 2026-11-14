package serviceaccounts

import "time"

// Account is a machine principal in a tenant.
type Account struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// Token is an API credential; the plaintext value is returned only once.
type Token struct {
	ID            string     `json:"id"`
	PrincipalType string     `json:"principalType"`
	PrincipalID   string     `json:"principalId"`
	Scopes        []string   `json:"scopes"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	LastUsedAt    *time.Time `json:"lastUsedAt"`
	RevokedAt     *time.Time `json:"revokedAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	Token         string     `json:"token,omitempty"`
}
