package audits

import (
	"context"
	"database/sql"
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

type AuditCitations struct {
	Total      int              `json:"total"`
	Official   int              `json:"official"`
	ThirdParty int              `json:"thirdParty"`
	Domains    []CitationDomain `json:"domains"`
	Items      []CitationItem   `json:"items"`
}

func (s *Store) Citations(ctx context.Context, tenantID, auditID string) (AuditCitations, error) {
	result := AuditCitations{Domains: []CitationDomain{}, Items: []CitationItem{}}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
COALESCE(SUM(c.source_type='official'),0),COALESCE(SUM(c.source_type='third_party'),0)
FROM citations c JOIN answer_evaluations ae ON ae.id=c.evaluation_id
JOIN ai_responses r ON r.id=ae.response_id JOIN audit_samples s ON s.id=r.sample_id
WHERE c.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&result.Total, &result.Official, &result.ThirdParty); err != nil {
		return result, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(c.domain,''),COUNT(*),
COALESCE(SUM(c.source_type='official'),0),COALESCE(SUM(c.source_type='third_party'),0),COUNT(DISTINCT r.id)
FROM citations c JOIN answer_evaluations ae ON ae.id=c.evaluation_id
JOIN ai_responses r ON r.id=ae.response_id JOIN audit_samples s ON s.id=r.sample_id
WHERE c.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)
GROUP BY COALESCE(c.domain,'') ORDER BY COUNT(*) DESC LIMIT 20`, tenantID, auditID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item CitationDomain
		if err := rows.Scan(&item.Domain, &item.Count, &item.Official, &item.ThirdParty, &item.Answers); err != nil {
			rows.Close()
			return result, err
		}
		result.Domains = append(result.Domains, item)
	}
	rows.Close()
	itemRows, err := s.db.QueryContext(ctx, `SELECT COALESCE(c.normalized_url,c.raw_url),COALESCE(c.domain,''),c.source_type,COALESCE(c.support_label,''),q.text_value,COALESCE(c.confidence,0)
FROM citations c JOIN answer_evaluations ae ON ae.id=c.evaluation_id
JOIN ai_responses r ON r.id=ae.response_id JOIN audit_samples s ON s.id=r.sample_id
JOIN questions q ON q.id=s.question_id
WHERE c.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)
ORDER BY c.created_at DESC,c.id DESC LIMIT 50`, tenantID, auditID)
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
