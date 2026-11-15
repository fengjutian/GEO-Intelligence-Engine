package audits

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// generateShareMetrics adds brand/competitor share values to one metric snapshot.
func generateShareMetrics(ctx context.Context, tx *sql.Tx, tenantID, auditID, snapshotID string) error {
	var projectID string
	if err := tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(project_id) FROM audits WHERE tenant_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, auditID).Scan(&projectID); err != nil {
		return err
	}
	type target struct {
		name  string
		kind  string
		terms []string
	}
	targets := []target{}
	var brandName string
	if err := tx.QueryRowContext(ctx, `SELECT canonical_name FROM brands WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY created_at,id LIMIT 1`, tenantID, projectID).Scan(&brandName); err == nil && brandName != "" {
		terms := []string{brandName}
		rows, err := tx.QueryContext(ctx, `SELECT a.alias FROM brand_aliases a JOIN brands b ON b.id=a.brand_id WHERE a.tenant_id=UUID_TO_BIN(?) AND b.project_id=UUID_TO_BIN(?) AND a.enabled=TRUE`, tenantID, projectID)
		if err == nil {
			for rows.Next() {
				var alias string
				if rows.Scan(&alias) == nil && alias != "" {
					terms = append(terms, alias)
				}
			}
			rows.Close()
		}
		targets = append(targets, target{name: brandName, kind: "brand", terms: terms})
	}
	rows, err := tx.QueryContext(ctx, `SELECT name,COALESCE(domain,'') FROM competitors WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status='active' ORDER BY name`, tenantID, projectID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name, domain string
		if err := rows.Scan(&name, &domain); err != nil {
			rows.Close()
			return err
		}
		terms := []string{name}
		if domain != "" {
			terms = append(terms, strings.TrimPrefix(domain, "www."))
		}
		targets = append(targets, target{name: name, kind: "competitor", terms: terms})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	answerRows, err := tx.QueryContext(ctx, `SELECT COALESCE(r.content_text,'') FROM ai_responses r
JOIN audit_samples s ON s.id=r.sample_id WHERE r.tenant_id=UUID_TO_BIN(?) AND s.audit_id=UUID_TO_BIN(?)`, tenantID, auditID)
	if err != nil {
		return err
	}
	answers := []string{}
	for answerRows.Next() {
		var content string
		if err := answerRows.Scan(&content); err != nil {
			answerRows.Close()
			return err
		}
		answers = append(answers, content)
	}
	answerRows.Close()
	if len(answers) == 0 {
		return nil
	}
	type value struct {
		name     string
		kind     string
		mentions int
		answers  int
	}
	values := make([]value, len(targets))
	totalMentions := 0
	for index, target := range targets {
		values[index] = value{name: target.name, kind: target.kind}
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
				values[index].mentions += count
				values[index].answers++
			}
		}
		totalMentions += values[index].mentions
	}
	for _, item := range values {
		dimensions, _ := json.Marshal(map[string]any{"entity": item.name, "entityType": item.kind})
		parsed := map[string]any{}
		_ = json.Unmarshal(dimensions, &parsed)
		if _, err := insertMetricValue(ctx, tx, tenantID, snapshotID, "share_of_voice", parsed, item.answers, len(answers)); err != nil {
			return err
		}
		if totalMentions > 0 {
			if _, err := insertMetricValue(ctx, tx, tenantID, snapshotID, "mention_share", parsed, item.mentions, totalMentions); err != nil {
				return err
			}
		}
	}
	return nil
}
