package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
	"github.com/google/uuid"
)

type competitiveStore interface {
	CompetitorCoverage(context.Context, string, string) (questions.QuestionCompetitorCoverage, error)
	LatestShareOfVoice(context.Context, string, string) (audits.ShareOfVoice, error)
	CompetitorAliases(context.Context, string, string) (map[string][]string, error)
}

type competitiveHandler struct {
	store       competitiveStore
	devTenantID string
}

type competitiveRow struct {
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	QuestionMentions int     `json:"questionMentions"`
	ComparisonCount  int     `json:"comparisonCount"`
	QuestionShare    float64 `json:"questionShare"`
	AnswerShare      float64 `json:"answerShare"`
	MentionShare     float64 `json:"mentionShare"`
	Gap              float64 `json:"gap"`
	Signal           string  `json:"signal"`
	Positive         int     `json:"positive"`
	Negative         int     `json:"negative"`
}

func newCompetitiveHandler(store competitiveStore, devTenantID string) *competitiveHandler {
	return &competitiveHandler{store: store, devTenantID: devTenantID}
}

func (h *competitiveHandler) get(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if _, err := uuid.Parse(projectID); err != nil {
		writeProblem(w, http.StatusBadRequest, "INVALID_PROJECT_ID", "Project ID is invalid", traceID(r))
		return
	}
	tenantID := r.Header.Get(headerTenant)
	if tenantID == "" {
		tenantID = h.devTenantID
	}
	coverage, err := h.store.CompetitorCoverage(r.Context(), tenantID, projectID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Unexpected server error", traceID(r))
		return
	}
	share, shareErr := h.store.LatestShareOfVoice(r.Context(), tenantID, projectID)
	aliases, _ := h.store.CompetitorAliases(r.Context(), tenantID, projectID)
	type shareProjection struct {
		share      float64
		mention    float64
		positive   int
		negative   int
		entityType string
	}
	byName := map[string]shareProjection{}
	for _, entity := range share.Entities {
		key := strings.ToLower(strings.TrimSpace(entity.Name))
		byName[key] = shareProjection{share: entity.Share, mention: entity.MentionShare, positive: entity.Positive, negative: entity.Negative, entityType: entity.Type}
	}
	lookup := func(competitorID, name string) shareProjection {
		terms := append([]string{name}, aliases[competitorID]...)
		for _, term := range terms {
			if value, ok := byName[strings.ToLower(strings.TrimSpace(term))]; ok {
				return value
			}
		}
		return shareProjection{}
	}
	rows := []competitiveRow{}
	for _, competitor := range coverage.Competitors {
		value := lookup(competitor.ID, competitor.Name)
		questionShare := 0.0
		if coverage.Total > 0 {
			questionShare = float64(competitor.Mentions) / float64(coverage.Total)
		}
		gap := value.mention - questionShare
		signal := "aligned"
		if gap >= 0.15 {
			signal = "strength"
		} else if gap <= -0.15 {
			signal = "opportunity"
		}
		rows = append(rows, competitiveRow{
			Name: competitor.Name, Type: "competitor", QuestionMentions: competitor.Mentions,
			ComparisonCount: competitor.ComparisonCount, QuestionShare: questionShare,
			AnswerShare: value.share, MentionShare: value.mention, Gap: gap,
			Signal: signal, Positive: value.positive, Negative: value.negative,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projectId": projectID, "questionTotal": coverage.Total, "hasShareData": shareErr == nil,
		"brandShare": brandShare(share), "rows": rows,
	})
}

func brandShare(share audits.ShareOfVoice) float64 {
	for _, entity := range share.Entities {
		if entity.Type == "brand" {
			return entity.Share
		}
	}
	return 0
}
