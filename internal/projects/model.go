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
