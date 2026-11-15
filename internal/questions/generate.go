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

var ErrNoKnowledgeSnapshot = errors.New("no knowledge snapshot is available")

// GenerateInput selects the frozen knowledge snapshot and question language.
type GenerateInput struct {
	KnowledgeSnapshotID string `json:"knowledgeSnapshotId"`
	Language            string `json:"language"`
	Mode                string `json:"mode,omitempty"`
	Provider            string `json:"provider,omitempty"`
	Model               string `json:"model,omitempty"`
}

type snapshot struct {
	Brand struct {
		CanonicalName string `json:"canonicalName"`
	} `json:"brand"`
	Products []struct {
		Name string `json:"name"`
	} `json:"products"`
	Competitors []struct {
		Name string `json:"name"`
	} `json:"competitors"`
	Entities []struct {
		CanonicalName string `json:"canonicalName"`
		Type          string `json:"type"`
	} `json:"entities"`
}

type candidate struct {
	text    string
	kind    string
	intent  string
	topic   string
	funnel  string
	basis   string
	branded bool
}

// Generate creates draft questions from a frozen knowledge snapshot using template-v1.
func (s *Store) Generate(ctx context.Context, tenantID, projectID string, input GenerateInput) (GenerationRun, []Question, error) {
	language := strings.TrimSpace(input.Language)
	if language == "" {
		language = "zh-CN"
	}
	snapshotID := strings.TrimSpace(input.KnowledgeSnapshotID)
	var content []byte
	if snapshotID != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT content_json FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) AND id=UUID_TO_BIN(?)`, tenantID, projectID, snapshotID).Scan(&content); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return GenerationRun{}, nil, ErrNoKnowledgeSnapshot
			}
			return GenerationRun{}, nil, err
		}
	} else {
		if err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id),content_json FROM knowledge_snapshots WHERE tenant_id=UUID_TO_BIN(?) AND project_id=UUID_TO_BIN(?) ORDER BY version DESC LIMIT 1`, tenantID, projectID).Scan(&snapshotID, &content); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return GenerationRun{}, nil, ErrNoKnowledgeSnapshot
			}
			return GenerationRun{}, nil, err
		}
	}
	var frozen snapshot
	if err := json.Unmarshal(content, &frozen); err != nil {
		return GenerationRun{}, nil, ErrNoKnowledgeSnapshot
	}
	candidates := generateCandidates(frozen, language)
	if len(candidates) == 0 {
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
	configHash := sha256.Sum256([]byte("template-v1|" + language + "|" + snapshotID))
	if _, err := tx.ExecContext(ctx, `INSERT INTO question_generation_runs (id,tenant_id,project_id,knowledge_snapshot_id,generator_version,status,config_hash,created_at,finished_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),'template-v1','succeeded',?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		runID, tenantID, projectID, snapshotID, configHash[:]); err != nil {
		return GenerationRun{}, nil, fmt.Errorf("insert generation run: %w", err)
	}
	created := []string{}
	skipped := 0
	existing, err := existingQuestionTexts(ctx, tx, versionID)
	if err != nil {
		return GenerationRun{}, nil, err
	}
	for _, item := range candidates {
		if existing[NormalizeText(item.text)] {
			skipped++
			continue
		}
		hash := sha256.Sum256([]byte(NormalizeText(item.text)))
		attributes, _ := json.Marshal(map[string]any{"source": "generated", "brandType": brandedLabel(item.branded), "generationRunId": runID, "basis": item.basis})
		id := uuid.NewString()
		_, err := tx.ExecContext(ctx, `INSERT INTO questions (id,tenant_id,version_id,text_value,text_hash,language,intent,topic,funnel_stage,question_type,weight,status,attributes_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,?,1,'draft',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
			id, tenantID, versionID, item.text, hash[:], language, nullable(item.intent), nullable(item.topic), nullable(item.funnel), item.kind, attributes)
		if err != nil {
			if strings.Contains(err.Error(), "1062") {
				skipped++
				continue
			}
			return GenerationRun{}, nil, fmt.Errorf("insert generated question: %w", err)
		}
		created = append(created, id)
		existing[NormalizeText(item.text)] = true
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
	return GenerationRun{ID: runID, Status: "succeeded", GeneratorVersion: "template-v1", Generated: len(items), Skipped: skipped}, items, nil
}

func generatedBranded(name, text string) bool {
	return name != "" && strings.Contains(strings.ToLower(text), strings.ToLower(name))
}

func brandedLabel(branded bool) string {
	if branded {
		return "branded"
	}
	return "non_branded"
}

func generateCandidates(frozen snapshot, language string) []candidate {
	zh := strings.HasPrefix(strings.ToLower(language), "zh")
	brand := strings.TrimSpace(frozen.Brand.CanonicalName)
	items := []candidate{}
	add := func(text, kind, intent, topic, funnel, basis string, branded bool) {
		text = strings.TrimSpace(text)
		if text != "" {
			items = append(items, candidate{text: text, kind: kind, intent: intent, topic: topic, funnel: funnel, basis: basis, branded: branded})
		}
	}
	if brand != "" {
		if zh {
			add(brand+" 是什么？", "brand", "definition", brand, "awareness", "brand", true)
			add(brand+" 靠谱吗？", "brand", "trust", brand, "consideration", "brand", true)
		} else {
			add("What is "+brand+"?", "brand", "definition", brand, "awareness", "brand", true)
			add("Is "+brand+" reliable?", "brand", "trust", brand, "consideration", "brand", true)
		}
	}
	for _, product := range frozen.Products {
		name := strings.TrimSpace(product.Name)
		if name == "" {
			continue
		}
		if zh {
			add(name+" 怎么用？", "scenario", "usage", name, "consideration", "product", generatedBranded(brand, name))
			add(name+" 适合哪些场景？", "scenario", "fit", name, "decision", "product", generatedBranded(brand, name))
		} else {
			add("How does "+name+" work?", "scenario", "usage", name, "consideration", "product", generatedBranded(brand, name))
			add("What is "+name+" best used for?", "scenario", "fit", name, "decision", "product", generatedBranded(brand, name))
		}
	}
	for _, competitor := range frozen.Competitors {
		name := strings.TrimSpace(competitor.Name)
		if name == "" || brand == "" {
			continue
		}
		if zh {
			add(brand+" 与 "+name+" 有什么区别？", "comparison", "comparison", brand+" vs "+name, "decision", "competitor", true)
		} else {
			add(brand+" vs "+name+": which is better?", "comparison", "comparison", brand+" vs "+name, "decision", "competitor", true)
		}
	}
	for _, entity := range frozen.Entities {
		name := strings.TrimSpace(entity.CanonicalName)
		if name == "" || strings.EqualFold(name, brand) {
			continue
		}
		if zh {
			add(name+" 是做什么的？", "category", "definition", name, "awareness", "entity", generatedBranded(brand, name))
		} else {
			add("What does "+name+" do?", "category", "definition", name, "awareness", "entity", generatedBranded(brand, name))
		}
	}
	return items
}
