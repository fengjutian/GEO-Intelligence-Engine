package audits

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
)

// ShareEntity is one brand or competitor in the answer set.
type ShareEntity struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Mentions     int     `json:"mentions"`
	Answers      int     `json:"answers"`
	Share        float64 `json:"share"`
	MentionShare float64 `json:"mentionShare"`
}

// ShareOfVoice is the answer-level presence share for a brand and its competitors.
type ShareOfVoice struct {
	AuditID      string        `json:"auditId"`
	TotalAnswers int           `json:"totalAnswers"`
	BrandShare   float64       `json:"brandShare"`
	Entities     []ShareEntity `json:"entities"`
}

type shareTarget struct {
	name  string
	kind  string
	terms []string
}

// ShareOfVoice counts brand and competitor mentions in stored answer text.
func (s *Store) ShareOfVoice(ctx context.Context, tenantID, auditID string) (ShareOfVoice, error) {
	var projectID string
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id) FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return ShareOfVoice{}, ErrAuditNotFound
	}
	if err != nil {
		return ShareOfVoice{}, err
	}
	targets := []shareTarget{}
	var brandName string
	if err := s.db.QueryRowContext(ctx, `SELECT canonical_name FROM brands WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1`, tenantID, projectID).Scan(&brandName); err == nil && brandName != "" {
		terms := []string{brandName}
		rows, aliasErr := s.db.QueryContext(ctx, `SELECT a.alias FROM brand_aliases a JOIN brands b ON b.id=a.brand_id WHERE a.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) AND a.enabled=TRUE`, tenantID, projectID)
		if aliasErr == nil {
			for rows.Next() {
				var alias string
				if rows.Scan(&alias) == nil && alias != "" {
					terms = append(terms, alias)
				}
			}
			rows.Close()
		}
		targets = append(targets, shareTarget{name: brandName, kind: "brand", terms: terms})
	}
	type competitorRow struct {
		id     string
		name   string
		domain string
	}
	competitors := []competitorRow{}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id),name,COALESCE(domain,'') FROM competitors WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status='active' ORDER BY name`, tenantID, projectID)
	if err != nil {
		return ShareOfVoice{}, err
	}
	for rows.Next() {
		var item competitorRow
		if err := rows.Scan(&item.id, &item.name, &item.domain); err != nil {
			rows.Close()
			return ShareOfVoice{}, err
		}
		competitors = append(competitors, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ShareOfVoice{}, err
	}
	for _, competitor := range competitors {
		terms := []string{competitor.name}
		if competitor.domain != "" {
			terms = append(terms, strings.TrimPrefix(competitor.domain, "www."))
		}
		aliasRows, err := s.db.QueryContext(ctx, `SELECT alias FROM competitor_aliases WHERE tenant_id=UUID_TO_BIN(?) AND competitor_id=UUID_TO_BIN(?) AND enabled=TRUE`, tenantID, competitor.id)
		if err == nil {
			for aliasRows.Next() {
				var alias string
				if aliasRows.Scan(&alias) == nil && alias != "" {
					terms = append(terms, alias)
				}
			}
			aliasRows.Close()
		}
		targets = append(targets, shareTarget{name: competitor.name, kind: "competitor", terms: terms})
	}
	answerRows, err := s.db.QueryContext(ctx, `SELECT COALESCE(r.content_text,'') FROM ai_responses r
JOIN audit_samples s ON s.id=r.sample_id WHERE r.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)`, tenantID, auditID)
	if err != nil {
		return ShareOfVoice{}, err
	}
	answers := []string{}
	for answerRows.Next() {
		var content string
		if err := answerRows.Scan(&content); err != nil {
			answerRows.Close()
			return ShareOfVoice{}, err
		}
		answers = append(answers, content)
	}
	answerRows.Close()
	result := ShareOfVoice{AuditID: auditID, TotalAnswers: len(answers), Entities: []ShareEntity{}}
	totalMentions := 0
	for _, target := range targets {
		entity := ShareEntity{Name: target.name, Type: target.kind}
		for _, answer := range answers {
			lowered := strings.ToLower(answer)
			count := 0
			for _, term := range target.terms {
				term = strings.ToLower(strings.TrimSpace(term))
				if term != "" {
					count += strings.Count(lowered, term)
				}
			}
			if count > 0 {
				entity.Mentions += count
				entity.Answers++
			}
		}
		totalMentions += entity.Mentions
		result.Entities = append(result.Entities, entity)
	}
	for index := range result.Entities {
		if result.TotalAnswers > 0 {
			result.Entities[index].Share = float64(result.Entities[index].Answers) / float64(result.TotalAnswers)
		}
		if totalMentions > 0 {
			result.Entities[index].MentionShare = float64(result.Entities[index].Mentions) / float64(totalMentions)
		}
		if result.Entities[index].Type == "brand" {
			result.BrandShare = result.Entities[index].Share
		}
	}
	sort.Slice(result.Entities, func(i, j int) bool { return result.Entities[i].Mentions > result.Entities[j].Mentions })
	return result, nil
}
