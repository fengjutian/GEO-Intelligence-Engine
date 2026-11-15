package drafts

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Draft is one generated optimization artifact awaiting approval.
type Draft struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"projectId"`
	PageContentID *string        `json:"pageContentId"`
	Kind          string         `json:"kind"`
	Status        string         `json:"status"`
	Title         string         `json:"title"`
	Payload       map[string]any `json:"payload"`
	Version       int            `json:"version"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

// Brand is the minimal brand context used by every generator.
type Brand struct {
	Name        string
	OfficialURL string
	Description string
}

// kinds are the artifacts the optimizer can generate today.
var kinds = map[string]bool{"jsonld": true, "llms_txt": true, "meta": true}

// ValidKind guards the generator input.
func ValidKind(kind string) bool { return kinds[kind] }

// BuildJSONLD renders Organization + Product structured data for the brand.
func BuildJSONLD(brand Brand) map[string]any {
	organization := map[string]any{
		"@type": "Organization",
		"name":  brand.Name,
		"url":   brand.OfficialURL,
	}
	if brand.Description != "" {
		organization["description"] = brand.Description
	}
	graph := []any{organization}
	if brand.Name != "" {
		graph = append(graph, map[string]any{
			"@type":         "Product",
			"name":          brand.Name,
			"brand":         map[string]any{"@type": "Brand", "name": brand.Name},
			"url":           brand.OfficialURL,
			"description":   brand.Description,
		})
	}
	return map[string]any{"@context": "https://schema.org", "@graph": graph}
}

// BuildLLMsTxt renders an llms.txt index with the pages AI crawlers should read.
func BuildLLMsTxt(brand Brand, urls []string) string {
	var builder strings.Builder
	builder.WriteString("# " + brand.Name + "\n\n")
	if brand.Description != "" {
		builder.WriteString("> " + brand.Description + "\n\n")
	}
	builder.WriteString("## Pages\n\n")
	unique := map[string]bool{}
	ordered := make([]string, 0, len(urls))
	for _, url := range urls {
		trimmed := strings.TrimSpace(url)
		if trimmed == "" || unique[trimmed] {
			continue
		}
		unique[trimmed] = true
		ordered = append(ordered, trimmed)
	}
	sort.Strings(ordered)
	for index, url := range ordered {
		if index >= 200 {
			break
		}
		builder.WriteString("- " + url + "\n")
	}
	return builder.String()
}

// BuildMeta proposes canonical, title and description tags for one page.
func BuildMeta(brand Brand, pageURL, pageTitle string) map[string]any {
	title := strings.TrimSpace(pageTitle)
	if title == "" {
		title = brand.Name
	}
	description := brand.Description
	if description == "" {
		description = fmt.Sprintf("%s 官方网站", brand.Name)
	}
	return map[string]any{
		"canonical":   pageURL,
		"title":       title,
		"description": description,
		"og:site_name": brand.Name,
	}
}

// Change is one field-level difference between two drafts.
type Change struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// Diff reports top-level payload changes, treating missing keys as nil.
func Diff(before, after map[string]any) []Change {
	changes := []Change{}
	keys := map[string]bool{}
	for key := range before {
		keys[key] = true
	}
	for key := range after {
		keys[key] = true
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		left, leftOK := before[key]
		right, rightOK := after[key]
		if leftOK && rightOK && fmt.Sprint(left) == fmt.Sprint(right) {
			continue
		}
		if !leftOK && !rightOK {
			continue
		}
		changes = append(changes, Change{Field: key, Before: left, After: right})
	}
	return changes
}
