package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls the Python Answer Judge service.
type Client struct {
	baseURL string
	client  *http.Client
}

func New(baseURL string) *Client {
	return &Client{baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), client: &http.Client{Timeout: 20 * time.Second}}
}

// Input is one answer to evaluate against a frozen question and brand snapshot.
type Input struct {
	Question        string   `json:"question"`
	Answer          string   `json:"answer"`
	BrandName       string   `json:"brand_name"`
	BrandDomain     string   `json:"brand_domain"`
	BrandAliases    []string `json:"brand_aliases"`
	Language        string   `json:"language"`
	TransportStatus string   `json:"transport_status"`
	FinishReason    string   `json:"finish_reason"`
}

// Mention is one brand occurrence with character offsets.
type Mention struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Citation is one URL referenced by the answer, classified as official or third-party.
type Citation struct {
	RawURL        string  `json:"raw_url"`
	NormalizedURL string  `json:"normalized_url"`
	Domain        string  `json:"domain"`
	SourceType    string  `json:"source_type"`
	Confidence    float64 `json:"confidence"`
}

// Result is the deterministic judge output persisted for metrics eligibility.
type Result struct {
	ContentValidity        string     `json:"content_validity"`
	AnswerOutcome          string     `json:"answer_outcome"`
	RecommendationLabel    string     `json:"recommendation_label"`
	Sentiment              string     `json:"sentiment"`
	Confidence             float64    `json:"confidence"`
	EligibleForCoreMetrics bool       `json:"eligible_for_core_metrics"`
	JudgeVersion           string     `json:"judge_version"`
	EligibilityRuleVersion string     `json:"eligibility_rule_version"`
	Mentions               []Mention  `json:"mentions"`
	Citations              []Citation `json:"citations"`
}

func (c *Client) Evaluate(ctx context.Context, input Input) (Result, error) {
	if input.BrandAliases == nil {
		input.BrandAliases = []string{}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return Result{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/judge/v1/evaluate", bytes.NewReader(payload))
	if err != nil {
		return Result{}, err
	}
	request.Header.Set("content-type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("call judge: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("judge returned HTTP %d", response.StatusCode)
	}
	var result Result
	if err := json.Unmarshal(body, &result); err != nil {
		return Result{}, fmt.Errorf("decode judge result: %w", err)
	}
	if result.JudgeVersion == "" || result.ContentValidity == "" {
		return Result{}, fmt.Errorf("judge returned an incomplete result")
	}
	return result, nil
}
