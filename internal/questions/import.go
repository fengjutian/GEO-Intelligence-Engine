package questions

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

var ErrInvalidImport = errors.New("question import is invalid")

// ImportRow is one CSV row preview/result.
type ImportRow struct {
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ImportResult is the two-phase CSV import response.
type ImportResult struct {
	Committed bool        `json:"committed"`
	Total     int         `json:"total"`
	Valid     int         `json:"valid"`
	Invalid   int         `json:"invalid"`
	Rows      []ImportRow `json:"rows"`
}

// Import parses and validates a question CSV. commit=false only previews.
func (s *Store) Import(ctx context.Context, tenantID, projectID, csvText string, commit bool) (ImportResult, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvText, "\uFEFF")))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return ImportResult{}, ErrInvalidImport
	}
	if len(records) < 2 {
		return ImportResult{}, ErrInvalidImport
	}
	if len(records)-1 > 1000 {
		return ImportResult{}, ErrInvalidImport
	}
	header := map[string]int{}
	for index, name := range records[0] {
		header[strings.ToLower(strings.TrimSpace(name))] = index
	}
	required := []string{"text", "language"}
	for _, name := range required {
		if _, ok := header[name]; !ok {
			return ImportResult{}, ErrInvalidImport
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()
	versionID, err := ensureDraftVersion(ctx, tx, tenantID, projectID)
	if err != nil {
		return ImportResult{}, err
	}
	existing := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT text_value FROM questions WHERE version_id=UUID_TO_BIN(?)`, versionID)
	if err != nil {
		return ImportResult{}, err
	}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			rows.Close()
			return ImportResult{}, err
		}
		hash := sha256.Sum256([]byte(NormalizeText(text)))
		existing[string(hash[:])] = true
	}
	rows.Close()
	result := ImportResult{Committed: commit, Rows: []ImportRow{}}
	seen := map[string]bool{}
	for index, record := range records[1:] {
		row := ImportRow{Line: index + 2}
		value := func(name string) string {
			position, ok := header[name]
			if !ok || position >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[position])
		}
		row.Text = value("text")
		if row.Text == "" {
			row.Status, row.Message = "error", "text is required"
		} else if len([]rune(row.Text)) > 2000 {
			row.Status, row.Message = "error", "text exceeds 2000 characters"
		} else if strings.ContainsRune("=+-@", []rune(row.Text)[0]) {
			row.Status, row.Message = "error", "formula injection is not allowed"
		}
		language := value("language")
		if row.Status == "" && language == "" {
			row.Status, row.Message = "error", "language is required"
		}
		questionType := value("questiontype")
		if row.Status == "" && questionType == "" {
			questionType = "category"
		}
		validTypes := map[string]bool{"brand": true, "category": true, "comparison": true, "scenario": true, "decision": true}
		if row.Status == "" && !validTypes[questionType] {
			row.Status, row.Message = "error", "questionType is invalid"
		}
		weight := 1.0
		if raw := value("weight"); raw != "" {
			parsed, err := strconv.ParseFloat(raw, 64)
			if err != nil || parsed < 0 || parsed > 1000 {
				row.Status, row.Message = "error", "weight must be between 0 and 1000"
			} else {
				weight = parsed
			}
		}
		hash := sha256.Sum256([]byte(NormalizeText(row.Text)))
		if row.Status == "" {
			if existing[string(hash[:])] || seen[string(hash[:])] {
				row.Status, row.Message = "duplicate", "question already exists in the draft"
			}
			seen[string(hash[:])] = true
		}
		if row.Status == "" {
			row.Status = "valid"
		}
		result.Total++
		if row.Status == "valid" {
			result.Valid++
		} else {
			result.Invalid++
		}
		result.Rows = append(result.Rows, row)
		if commit && row.Status == "valid" {
			id := uuid.NewString()
			branded := "non_branded"
			if isBrandedQuestion(ctx, tx, tenantID, projectID, row.Text) {
				branded = "branded"
			}
			attributes, _ := json.Marshal(map[string]any{"source": "import", "brandType": branded})
			_, err := tx.ExecContext(ctx, `INSERT INTO questions (id,tenant_id,version_id,text_value,text_hash,language,intent,topic,funnel_stage,question_type,weight,status,attributes_json,created_at)
VALUES (UUID_TO_BIN(?),UUID_TO_BIN(?),UUID_TO_BIN(?),?,?,?,?,?,?,?,?,'draft',CAST(? AS JSON),UTC_TIMESTAMP(6))`,
				id, tenantID, versionID, row.Text, hash[:], language, nullable(value("intent")), nullable(value("topic")), nullable(value("funnelstage")), questionType, weight, attributes)
			if err != nil {
				return ImportResult{}, fmt.Errorf("insert imported question: %w", err)
			}
		}
	}
	if commit {
		if err := tx.Commit(); err != nil {
			return ImportResult{}, err
		}
	}
	return result, nil
}
