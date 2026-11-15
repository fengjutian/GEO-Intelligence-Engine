package httpapi

import (
	"context"

	"github.com/fengjutian/geo-intelligence-engine/internal/audits"
	"github.com/fengjutian/geo-intelligence-engine/internal/questions"
)

type competitiveStoreAdapter struct {
	questions *questions.Store
	audits    *audits.Store
}

func (a competitiveStoreAdapter) CompetitorCoverage(ctx context.Context, tenantID, projectID string) (questions.QuestionCompetitorCoverage, error) {
	return a.questions.CompetitorCoverage(ctx, tenantID, projectID)
}

func (a competitiveStoreAdapter) LatestShareOfVoice(ctx context.Context, tenantID, projectID string) (audits.ShareOfVoice, error) {
	return a.audits.LatestShareOfVoice(ctx, tenantID, projectID)
}
