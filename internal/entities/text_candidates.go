package entities

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// TextCandidate is an entity candidate mined from visible page prose.
type TextCandidate struct {
	Candidate
	Quote  string
	Offset int
}

var (
	englishDefinition = regexp.MustCompile(`^([A-Z][A-Za-z0-9&.'\-]*(?: [A-Z][A-Za-z0-9&.'\-]*){0,4})\s+(?:is|are)\s+(?:a|an|the)\s+([A-Za-z][A-Za-z\- ]{2,40})`)
	chineseDefinition = regexp.MustCompile(`^([\p{Han}A-Za-z0-9]{2,20})(?:是|是一款|是一个)([^。；，,.!?！？]{2,30})`)
	sentenceSplitter  = regexp.MustCompile(`[.!?。！？\n]+`)
)

var descriptorTypes = []struct {
	keywords []string
	kind     string
}{
	{[]string{"software", "platform", "tool", "application", "app", "saas", "软件", "平台", "工具", "应用"}, "SoftwareApplication"},
	{[]string{"company", "organization", "agency", "brand", "business", "公司", "组织", "机构", "品牌", "企业"}, "Organization"},
	{[]string{"product", "service", "offering", "产品", "服务"}, "Product"},
}

var sentenceStopwords = map[string]bool{
	"this": true, "that": true, "it": true, "there": true, "these": true, "those": true,
	"he": true, "she": true, "they": true, "we": true, "you": true, "i": true,
	"why": true, "what": true, "how": true, "when": true, "where": true, "which": true,
}

// ParseTextCandidates finds definition sentences such as "Acme is a GEO platform"
// and returns at most limit typed candidates with their evidence.
func ParseTextCandidates(text string, limit int) []TextCandidate {
	if limit <= 0 {
		return nil
	}
	candidates := []TextCandidate{}
	seen := map[string]bool{}
	offset := 0
	for _, sentence := range sentenceSplitter.Split(text, -1) {
		trimmed := strings.TrimSpace(sentence)
		start := offset
		offset += len(sentence) + 1
		if len(trimmed) < 8 {
			continue
		}
		name, entityType, ok := definitionCandidate(trimmed)
		if !ok {
			continue
		}
		key := normalizeName(name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, TextCandidate{
			Candidate: Candidate{Name: name, Type: entityType, Locator: "text:def"},
			Quote:     truncateRunes(trimmed, 240),
			Offset:    start,
		})
		if len(candidates) >= limit {
			break
		}
	}
	return candidates
}

func definitionCandidate(sentence string) (string, string, bool) {
	if matches := chineseDefinition.FindStringSubmatch(sentence); len(matches) == 3 {
		if entityType, ok := entityTypeFromDescriptor(matches[2]); ok {
			return strings.TrimSpace(matches[1]), entityType, true
		}
	}
	if matches := englishDefinition.FindStringSubmatch(sentence); len(matches) == 3 {
		name := strings.TrimSpace(matches[1])
		if sentenceStopwords[strings.ToLower(name)] || len(name) < 3 {
			return "", "", false
		}
		if entityType, ok := entityTypeFromDescriptor(matches[2]); ok {
			return name, entityType, true
		}
	}
	return "", "", false
}

func entityTypeFromDescriptor(descriptor string) (string, bool) {
	lower := strings.ToLower(descriptor)
	for _, entry := range descriptorTypes {
		for _, keyword := range entry.keywords {
			if strings.Contains(lower, keyword) {
				return entry.kind, true
			}
		}
	}
	return "", false
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// ExtractText adds prose evidence for known entities and persists low-confidence
// definition candidates. Structured data always wins: this never overwrites an
// existing entity or a human-confirmed status.
func ExtractText(ctx context.Context, tx *sql.Tx, tenantID, projectID, contentID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	text = truncateRunes(text, 4000)
	lowerText := strings.ToLower(text)
	known := map[string]string{}
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id),canonical_name FROM entities WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`, tenantID, projectID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return err
		}
		known[strings.ToLower(name)] = id
	}
	rows.Close()
	aliasRows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(ea.entity_id),ea.alias FROM entity_aliases ea JOIN entities e ON e.id=ea.entity_id
WHERE ea.tenant_id=UUID_TO_BIN(?) AND e.project_id=UUID_TO_BIN(?)`, tenantID, projectID)
	if err != nil {
		return err
	}
	for aliasRows.Next() {
		var id, alias string
		if err := aliasRows.Scan(&id, &alias); err != nil {
			aliasRows.Close()
			return err
		}
		known[strings.ToLower(alias)] = id
	}
	aliasRows.Close()

	matched := 0
	for name, entityID := range known {
		if matched >= 40 {
			break
		}
		index := findWord(lowerText, name)
		if index < 0 {
			continue
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_evidence WHERE tenant_id=UUID_TO_BIN(?) AND target_id=UUID_TO_BIN(?) AND source_id=UUID_TO_BIN(?) AND source_type='page_text'`,
			tenantID, entityID, contentID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		evidenceID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO entity_evidence (id,tenant_id,target_type,target_id,source_type,source_id,quote_text,locator,extractor_version,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'entity',UUID_TO_BIN(?),'page_text',UUID_TO_BIN(?),?,?,'text-v1',UTC_TIMESTAMP(6))`,
			evidenceID, tenantID, entityID, contentID, quoteAround(text, index, len(name)), "text:"+name); err != nil {
			return err
		}
		matched++
	}

	for _, candidate := range ParseTextCandidates(text, 5) {
		key := normalizeName(candidate.Name)
		if _, ok := known[key]; ok {
			continue
		}
		entityID, err := upsertEntity(ctx, tx, tenantID, projectID, candidate.Candidate)
		if err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entity_evidence WHERE tenant_id=UUID_TO_BIN(?) AND target_id=UUID_TO_BIN(?) AND source_id=UUID_TO_BIN(?) AND extractor_version='text-v1'`,
			tenantID, entityID, contentID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		evidenceID := uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO entity_evidence (id,tenant_id,target_type,target_id,source_type,source_id,quote_text,locator,extractor_version,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),'entity',UUID_TO_BIN(?),'page_text',UUID_TO_BIN(?),?,?,'text-v1',UTC_TIMESTAMP(6))`,
			evidenceID, tenantID, entityID, contentID, candidate.Quote, candidate.Locator); err != nil {
			return err
		}
	}
	return nil
}

// quoteAround expands a byte offset into a human-readable evidence window.
func quoteAround(text string, byteIndex, byteLength int) string {
	start := byteIndex - 120
	if start < 0 {
		start = 0
	}
	for start > 0 && start < len(text) && !utf8.RuneStart(text[start]) {
		start--
	}
	end := byteIndex + byteLength + 120
	if end > len(text) {
		end = len(text)
	}
	for end > 0 && end < len(text) && !utf8.RuneStart(text[end]) {
		end--
	}
	return truncateRunes(strings.TrimSpace(text[start:end]), 240)
}

// findWord returns the first match of lowerName in lowerText that is not glued
// to a neighbouring letter or digit, so "Acme" does not match "Acmecorp".
func findWord(lowerText, lowerName string) int {
	offset := 0
	for {
		index := strings.Index(lowerText[offset:], lowerName)
		if index < 0 {
			return -1
		}
		index += offset
		before := index == 0 || !isWordRune(rune(lowerText[index-1]))
		afterIndex := index + len(lowerName)
		after := afterIndex >= len(lowerText) || !isWordRune(rune(lowerText[afterIndex]))
		if before && after {
			return index
		}
		offset = index + 1
	}
}

func isWordRune(value rune) bool { return unicode.IsLetter(value) || unicode.IsDigit(value) }
