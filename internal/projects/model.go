package projects

import "time"

type CreateInput struct {
	Name        string `json:"name"`
	BrandName   string `json:"brandName"`
	OfficialURL string `json:"officialUrl"`
	Locale      string `json:"locale"`
	Region      string `json:"region"`
	Timezone    string `json:"timezone"`
}

type PatchInput struct {
	Name     *string `json:"name"`
	Locale   *string `json:"locale"`
	Region   *string `json:"region"`
	Timezone *string `json:"timezone"`
}

type JobRef struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

type ConfigVersion struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"projectId"`
	Version     uint32    `json:"version"`
	ContentHash string    `json:"contentHash"`
	PublishedAt time.Time `json:"publishedAt"`
}

type BrandAlias struct {
	ID        string    `json:"id"`
	Alias     string    `json:"alias"`
	AliasType string    `json:"aliasType"`
	Locale    *string   `json:"locale"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
}

type AliasCreateInput struct {
	Alias     string  `json:"alias"`
	AliasType string  `json:"aliasType"`
	Locale    *string `json:"locale"`
}

type Product struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Description        *string   `json:"description"`
	VerificationStatus string    `json:"verificationStatus"`
	SourceType         string    `json:"sourceType"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type ProductCreateInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type BrandProfile struct {
	ProjectID      string       `json:"projectId"`
	BrandID        string       `json:"brandId"`
	CanonicalName  string       `json:"canonicalName"`
	OfficialDomain string       `json:"officialDomain"`
	Aliases        []BrandAlias `json:"aliases"`
	Products       []Product    `json:"products"`
	Competitors    []Competitor `json:"competitors"`
}

type Competitor struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Domain    *string   `json:"domain"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CompetitorCreateInput struct {
	Name   string  `json:"name"`
	Domain *string `json:"domain"`
}

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	BrandName   string    `json:"brandName"`
	OfficialURL string    `json:"officialUrl"`
	Locale      string    `json:"locale"`
	Region      string    `json:"region"`
	Timezone    string    `json:"timezone"`
	Status      string    `json:"status"`
	Version     uint64    `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Page struct {
	Items []Project `json:"items"`
	Page  PageMeta  `json:"page"`
}

type PageMeta struct {
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}
