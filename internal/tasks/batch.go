package tasks

import (
	"context"
	"fmt"
	"strings"
)

// BatchInput controls which open findings become optimization tasks.
type BatchInput struct {
	RuleCodes       []string `json:"ruleCodes"`
	Status          string   `json:"status"`
	AcceptanceItems []string `json:"acceptanceItems"`
	SkipExisting    *bool    `json:"skipExisting"`
}

// BatchItem reports one rule group.
type BatchItem struct {
	RuleCode     string `json:"ruleCode"`
	FindingCount int    `json:"findingCount"`
	TaskID       string `json:"taskId,omitempty"`
	Title        string `json:"title"`
	Created      bool   `json:"created"`
}

// BatchResult is the outcome of a batch task creation.
type BatchResult struct {
	Created int         `json:"created"`
	Skipped int         `json:"skipped"`
	Items   []BatchItem `json:"items"`
}

// BatchTitle is the deterministic title used for idempotent batch creation.
func BatchTitle(ruleCode string, count int) string {
	return fmt.Sprintf("[%s] %d 条问题待修复", ruleCode, count)
}

// BatchAcceptance builds default acceptance items for one rule group.
func BatchAcceptance(ruleCode string, count int, custom []string) []string {
	if len(custom) > 0 {
		return custom
	}
	return []string{
		fmt.Sprintf("复测后 %s 的 open finding 归零", ruleCode),
		fmt.Sprintf("修复覆盖本次 %d 条 finding 涉及的全部页面", count),
		"无新增同类 finding 回归",
	}
}

// BatchFromFindings groups open findings by rule and creates one task per rule.
func (s *Store) BatchFromFindings(ctx context.Context, tenantID, actorID, projectID string, input BatchInput) (BatchResult, error) {
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "open"
	}
	skipExisting := true
	if input.SkipExisting != nil {
		skipExisting = *input.SkipExisting
	}
	rows, err := s.db.QueryContext(ctx, `SELECT rule_code,COUNT(*) FROM findings
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status=?
GROUP BY rule_code ORDER BY rule_code`, tenantID, projectID, status)
	if err != nil {
		return BatchResult{}, err
	}
	type group struct {
		ruleCode string
		count    int
	}
	groups := []group{}
	allowed := map[string]bool{}
	for _, code := range input.RuleCodes {
		allowed[strings.TrimSpace(code)] = true
	}
	for rows.Next() {
		var item group
		if err := rows.Scan(&item.ruleCode, &item.count); err != nil {
			rows.Close()
			return BatchResult{}, err
		}
		if len(allowed) > 0 && !allowed[item.ruleCode] {
			continue
		}
		groups = append(groups, item)
	}
	rows.Close()

	result := BatchResult{Items: []BatchItem{}}
	for _, item := range groups {
		title := BatchTitle(item.ruleCode, item.count)
		entry := BatchItem{RuleCode: item.ruleCode, FindingCount: item.count, Title: title}
		if skipExisting {
			var existing int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimization_tasks
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND title=? AND status NOT IN ('done','cancelled')`,
				tenantID, projectID, title).Scan(&existing); err != nil {
				return BatchResult{}, err
			}
			if existing > 0 {
				result.Skipped++
				result.Items = append(result.Items, entry)
				continue
			}
		}
		var findingID string
		if err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id) FROM findings
WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND status=? AND rule_code=?
ORDER BY created_at LIMIT 1`, tenantID, projectID, status, item.ruleCode).Scan(&findingID); err != nil {
			return BatchResult{}, err
		}
		task, err := s.CreateFromFinding(ctx, tenantID, actorID, findingID, CreateInput{
			Title:           title,
			Description:     fmt.Sprintf("按规则 %s 归并 %d 条 open finding，修复后复测。", item.ruleCode, item.count),
			AcceptanceItems: BatchAcceptance(item.ruleCode, item.count, input.AcceptanceItems),
		})
		if err != nil {
			return BatchResult{}, err
		}
		entry.TaskID, entry.Created = task.ID, true
		result.Created++
		result.Items = append(result.Items, entry)
	}
	return result, nil
}
