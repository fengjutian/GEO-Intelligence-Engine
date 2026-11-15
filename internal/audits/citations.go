package audits

import (
	"context"
	"database/sql"
	"strings"
)

type CitationDomain struct {
	Domain     string `json:"domain"`
	Count      int    `json:"count"`
	Official   int    `json:"official"`
	ThirdParty int    `json:"thirdParty"`
	Answers    int    `json:"answers"`
}

type CitationItem struct {
	URL        string  `json:"url"`
	Domain     string  `json:"domain"`
	SourceType string  `json:"sourceType"`
	Support    string  `json:"supportLabel"`
	Question   string  `json:"question"`
	Confidence float64 `json:"confidence"`
}

type CitationSlice struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Total      int    `json:"total"`
	Official   int    `json:"official"`
	ThirdParty int    `json:"thirdParty"`
}

type CitationQuestionSlice struct {
	QuestionID string `json:"questionId"`
	Text       string `json:"text"`
	Total      int    `json:"total"`
	Official   int    `json:"official"`
	ThirdParty int    `json:"thirdParty"`
}

type AuditCitations struct {
	Total               int                     `json:"total"`
	Official            int                     `json:"official"`
	ThirdParty          int                     `json:"thirdParty"`
	BrandDomain         string                  `json:"brandDomain"`
	BrandDomainCount    int                     `json:"brandDomainCount"`
	BrandDomainCoverage float64                 `json:"brandDomainCoverage"`
	Domains             []CitationDomain        `json:"domains"`
	ByEngine            []CitationSlice         `json:"byEngine"`
	ByQuestion          []CitationQuestionSlice `json:"byQuestion"`
	Items               []CitationItem          `json:"items"`
}

// Citations aggregates citations for one audit, optionally sliced by engine or question.
func (s *Store) Citations(ctx context.Context, tenantID, auditID, engineConfigID, questionID string) (AuditCitations, error) {
	result := AuditCitations{Domains: []CitationDomain{}, ByEngine: []CitationSlice{}, ByQuestion: []CitationQuestionSlice{}, Items: []CitationItem{}}
	where := `c.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)`
	args := []any{tenantID, auditID}
	if engineConfigID != "" {
		where += " AND s.engine_config_id=UUID_TO_BIN(?)"
		args = append(args, engineConfigID)
	}
	if questionID != "" {
		where += " AND s.question_id=UUID_TO_BIN(?)"
		args = append(args, questionID)
	}
	base := ` FROM citations c JOIN answer_evaluations ae ON ae.id=c.evaluation_id
JOIN ai_responses r ON r.id=ae.response_id JOIN audit_samples s ON s.id=r.sample_id
JOIN questions q ON q.id=s.question_id JOIN engine_configs e ON e.id=s.engine_config_id WHERE ` + where
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(c.source_type='official'),0),COALESCE(SUM(c.source_type='third_party'),0)`+base, args...).Scan(&result.Total, &result.Official, &result.ThirdParty); err != nil {
		return result, err
	}
	if result.Total == 0 {
		return result, nil
	}
	var officialDomain string
	_ = s.db.QueryRowContext(ctx, `SELECT official_domain FROM brands WHERE tenant_id=UUID_TO_BIN(?) AND project_id=(SELECT project_id FROM audits WHERE id=UUID_TO_BIN(?)) ORDER BY created_at,id LIMIT 1`, tenantID, auditID).Scan(&officialDomain)
	result.BrandDomain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(officialDomain)), "www.")
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(c.domain,''),COUNT(*),COALESCE(SUM(c.source_type='official'),0),COALESCE(SUM(c.source_type='third_party'),0),COUNT(DISTINCT r.id)`+base+` GROUP BY COALESCE(c.domain,'') ORDER BY COUNT(*) DESC LIMIT 20`, args...)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item CitationDomain
		if err := rows.Scan(&item.Domain, &item.Count, &item.Official, &item.ThirdParty, &item.Answers); err != nil {
			rows.Close()
			return result, err
		}
		if domainMatches(item.Domain, result.BrandDomain) {
			result.BrandDomainCount += item.Count
		}
		result.Domains = append(result.Domains, item)
	}
	rows.Close()
	result.BrandDomainCoverage = float64(result.BrandDomainCount) / float64(result.Total)
	engineRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(s.engine_config_id),e.provider,e.channel,COUNT(*),COALESCE(SUM(c.source_type='official'),0),COALESCE(SUM(c.source_type='third_party'),0)`+base+`
GROUP BY s.engine_config_id,e.provider,e.channel ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		return result, err
	}
	for engineRows.Next() {
		var key, provider, channel string
		var item CitationSlice
		if err := engineRows.Scan(&key, &provider, &channel, &item.Total, &item.Official, &item.ThirdParty); err != nil {
			engineRows.Close()
			return result, err
		}
		item.Key = key
		item.Label = provider + " / " + channel
		result.ByEngine = append(result.ByEngine, item)
	}
	engineRows.Close()
	questionRows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(q.id),q.text_value,COUNT(*),COALESCE(SUM(c.source_type='official'),0),COALESCE(SUM(c.source_type='third_party'),0)`+base+`
GROUP BY q.id,q.text_value ORDER BY COUNT(*) DESC LIMIT 30`, args...)
	if err != nil {
		return result, err
	}
	for questionRows.Next() {
		var item CitationQuestionSlice
		if err := questionRows.Scan(&item.QuestionID, &item.Text, &item.Total, &item.Official, &item.ThirdParty); err != nil {
			questionRows.Close()
			return result, err
		}
		result.ByQuestion = append(result.ByQuestion, item)
	}
	questionRows.Close()
	itemRows, err := s.db.QueryContext(ctx, `SELECT COALESCE(c.normalized_url,c.raw_url),COALESCE(c.domain,''),c.source_type,COALESCE(c.support_label,''),q.text_value,COALESCE(c.confidence,0)`+base+`
ORDER BY c.created_at DESC,c.id DESC LIMIT 50`, args...)
	if err != nil {
		return result, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var item CitationItem
		var confidence sql.NullFloat64
		if err := itemRows.Scan(&item.URL, &item.Domain, &item.SourceType, &item.Support, &item.Question, &confidence); err != nil {
			return result, err
		}
		if confidence.Valid {
			item.Confidence = confidence.Float64
		}
		result.Items = append(result.Items, item)
	}
	return result, itemRows.Err()
}

func domainMatches(domain, official string) bool {
	domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), "www.")
	return official != "" && (domain == official || strings.HasSuffix(domain, "."+official))
}
