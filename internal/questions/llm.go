package questions

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var ErrNoCandidates = errors.New("no question candidates were produced")

// LoadGenerationContext reads the frozen snapshot used for LLM prompting.
func (s *Store) LoadGenerationContext(ctx context.Context, tenantID, projectID, snapshotID string) (GenerationContext, error) {
	query := `SELECT BIN_TO_UUID(id),content_json FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?)`
	args := []any{tenantID, projectID}
	if strings.TrimSpace(snapshotID) != "" {
		query += " AND id=UUID_TO_BIN(?)"
		args = append(args, snapshotID)
	} else {
		query += " ORDER BY version DESC LIMIT 1"
	}
	var id string
	var content []byte
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&id, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GenerationContext{}, ErrNoKnowledgeSnapshot
		}
		return GenerationContext{}, err
	}
	var frozen snapshot
	if err := json.Unmarshal(content, &frozen); err != nil {
		return GenerationContext{}, ErrNoKnowledgeSnapshot
	}
	result := GenerationContext{SnapshotID: id, Brand: frozen.Brand.CanonicalName}
	for _, product := range frozen.Products {
		result.Products = append(result.Products, product.Name)
	}
	for _, competitor := range frozen.Competitors {
		result.Competitors = append(result.Competitors, competitor.Name)
	}
	for _, entity := range frozen.Entities {
		result.Entities = append(result.Entities, entity.CanonicalName)
	}
	return result, nil
}

// BuildLLMPrompt asks for strict JSON buyer questions grounded in the snapshot.
func BuildLLMPrompt(input GenerationContext, language string) string {
	if language == "" {
		language = "zh-CN"
	}
	var builder strings.Builder
	builder.WriteString("You generate buyer questions for a GEO (generative engine optimization) audit.\n")
	builder.WriteString("Use only the provided brand knowledge. Do not invent facts, metrics, or search volume.\n")
	builder.WriteString("Return a strict JSON array, with no markdown or commentary. Each item must have: ")
	builder.WriteString(`{"text":"...","questionType":"brand|category|comparison|scenario|decision","intent":"...","topic":"...","funnelStage":"awareness|consideration|decision","basis":"brand|product|competitor|entity"}.`)
	builder.WriteString("\nGenerate 6 to 12 distinct questions in language " + language + ".\n")
	builder.WriteString("Brand: " + input.Brand + "\n")
	builder.WriteString("Products: " + strings.Join(input.Products, ", ") + "\n")
	builder.WriteString("Competitors: " + strings.Join(input.Competitors, ", ") + "\n")
	builder.WriteString("Entities: " + strings.Join(input.Entities, ", ") + "\n")
	return builder.String()
}

// ParseLLMCandidates extracts and validates candidate questions from model text.
func ParseLLMCandidates(content string, brandedBrand string) ([]Candidate, error) {
	value := strings.TrimSpace(content)
	value = strings.TrimPrefix(value, "```json")
	value = strings.TrimPrefix(value, "```")
	value = strings.TrimSuffix(value, "```")
	start := strings.Index(value, "[")
	end := strings.LastIndex(value, "]")
	if start < 0 || end <= start {
		return nil, ErrNoCandidates
	}
	var raw []Candidate
	if err := json.Unmarshal([]byte(value[start:end+1]), &raw); err != nil {
		return nil, ErrNoCandidates
	}
	validTypes := map[string]bool{"brand": true, "category": true, "comparison": true, "scenario": true, "decision": true}
	validFunnels := map[string]bool{"awareness": true, "consideration": true, "decision": true, "": true}
	seen := map[string]bool{}
	result := []Candidate{}
	for _, item := range raw {
		text := strings.TrimSpace(item.Text)
		item.QuestionType = normalizeQuestionType(item.QuestionType)
		item.FunnelStage = normalizeFunnelStage(item.FunnelStage)
		if text == "" || len([]rune(text)) > 2000 || !validTypes[item.QuestionType] || !validFunnels[item.FunnelStage] {
			continue
		}
		normalized := NormalizeText(text)
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		item.Text = text
		item.Branded = brandedBrand != "" && strings.Contains(strings.ToLower(text), strings.ToLower(brandedBrand))
		if item.Basis == "" {
			item.Basis = "llm"
		}
		result = append(result, item)
		if len(result) >= 20 {
			break
		}
	}
	if len(result) == 0 {
		return nil, ErrNoCandidates
	}
	return result, nil
}

func normalizeQuestionType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "brand", "品牌", "brand_understanding", "brand awareness":
		return "brand"
	case "comparison", "比较", "compare":
		return "comparison"
	case "scenario", "场景":
		return "scenario"
	case "decision", "决策", "purchase":
		return "decision"
	case "":
		return "category"
	default:
		return ""
	}
}

func normalizeFunnelStage(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "awareness", "认知", "top":
		return "awareness"
	case "consideration", "考虑", "middle":
		return "consideration"
	case "decision", "决策", "bottom":
		return "decision"
	default:
		return "awareness"
	}
}

// GenerateFromCandidates persists LLM/template candidates as draft questions.
func (s *Store) GenerateFromCandidates(ctx context.Context, tenantID, projectID, snapshotID, generatorVersion, model, language string, candidates []Candidate) (GenerationRun, []Question, error) {
	if len(candidates) == 0 {
		return GenerationRun{}, nil, ErrNoCandidates
	}
	var snapshotExists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID, snapshotID).Scan(&snapshotExists); err != nil {
		return GenerationRun{}, nil, err
	}
	if snapshotExists == 0 {
		return GenerationRun{}, nil, ErrNoKnowledgeSnapshot
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return GenerationRun{}, nil, err
	}
	defer tx.Rollback()
	versionID, err := ensureDraftVersion(ctx, tx, tenantID, projectID)
	if err != nil {
		return GenerationRun{}, nil, err
	}
	runID := uuid.NewString()
	configHash := sha256.Sum256([]byte(generatorVersion + "|" + model + "|" + snapshotID))
	if _, err := tx.ExecContext(ctx, `INSERT INTO question_generation_runs (id,tenant_id,project_id,knowledge_snapshot_id,generator_version,status,config_hash,created_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,'succeeded',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		runID, tenantID, projectID, snapshotID, generatorVersion, configHash[:]); err != nil {
		return GenerationRun{}, nil, fmt.Errorf("insert generation run: %w", err)
	}
	created := []string{}
	skipped := 0
	existing, err := existingQuestionTexts(ctx, tx, versionID)
	if err != nil {
		return GenerationRun{}, nil, err
	}
	for _, item := range candidates {
		if existing[NormalizeText(item.Text)] {
			skipped++
			continue
		}
		hash := sha256.Sum256([]byte(NormalizeText(item.Text)))
		attributes, _ := json.Marshal(map[string]any{
			"source": "generated", "brandType": brandedLabel(item.Branded), "generationRunId": runID,
			"basis": item.Basis, "generatorVersion": generatorVersion, "model": model,
		})
		id := uuid.NewString()
		_, err := tx.ExecContext(ctx, `INSERT INTO questions (id,tenant_id,version_id,text_value,text_hash,language,intent,topic,funnel_stage,question_type,weight,status,attributes_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,?,1,'draft',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
			id, tenantID, versionID, item.Text, hash[:], language, nullable(item.Intent), nullable(item.Topic), nullable(item.FunnelStage), item.QuestionType, attributes)
		if err != nil {
			if strings.Contains(err.Error(), "1062") {
				skipped++
				continue
			}
			return GenerationRun{}, nil, fmt.Errorf("insert generated question: %w", err)
		}
		created = append(created, id)
		existing[NormalizeText(item.Text)] = true
	}
	if err := tx.Commit(); err != nil {
		return GenerationRun{}, nil, err
	}
	items := make([]Question, 0, len(created))
	for _, id := range created {
		question, err := s.get(ctx, tenantID, id)
		if err != nil {
			return GenerationRun{}, nil, err
		}
		items = append(items, question)
	}
	return GenerationRun{ID: runID, Status: "succeeded", GeneratorVersion: generatorVersion, Model: model, Generated: len(items), Skipped: skipped}, items, nil
}
