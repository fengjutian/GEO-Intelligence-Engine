package experiments

import (
	"math"
	"time"
)

// Counts is a metric numerator/denominator pair for one audit.
type Counts struct {
	Numerator   int
	Denominator int
}

// Result is the statistical comparison of baseline and treatment.
type Result struct {
	BaselineRate float64 `json:"baselineRate"`
	TreatmentRate float64 `json:"treatmentRate"`
	Uplift       float64 `json:"uplift"`
	Z            float64 `json:"z"`
	PValue       float64 `json:"pValue"`
	Significant  bool    `json:"significant"`
	Insufficient bool    `json:"insufficient"`
	Direction    string  `json:"direction"`
}

// MinPerGroup keeps under-powered audits from declaring a winner.
const MinPerGroup = 30

// Alpha is the two-sided significance threshold.
const Alpha = 0.05

// Compare runs a two-proportion z-test between two audits.
func Compare(baseline, treatment Counts) Result {
	result := Result{}
	if baseline.Denominator > 0 {
		result.BaselineRate = float64(baseline.Numerator) / float64(baseline.Denominator)
	}
	if treatment.Denominator > 0 {
		result.TreatmentRate = float64(treatment.Numerator) / float64(treatment.Denominator)
	}
	result.Uplift = result.TreatmentRate - result.BaselineRate
	result.Direction = "none"
	if baseline.Denominator < MinPerGroup || treatment.Denominator < MinPerGroup {
		result.Insufficient = true
		return result
	}
	pooled := float64(baseline.Numerator+treatment.Numerator) / float64(baseline.Denominator+treatment.Denominator)
	standardError := math.Sqrt(pooled * (1 - pooled) * (1/float64(baseline.Denominator) + 1/float64(treatment.Denominator)))
	if standardError == 0 {
		result.Insufficient = true
		return result
	}
	result.Z = (result.TreatmentRate - result.BaselineRate) / standardError
	result.PValue = math.Erfc(math.Abs(result.Z) / math.Sqrt2)
	result.Significant = result.PValue < Alpha
	switch {
	case !result.Significant:
		result.Direction = "none"
	case result.Uplift > 0:
		result.Direction = "positive"
	default:
		result.Direction = "negative"
	}
	return result
}

// Conclusion turns a comparison into the experiment verdict.
func Conclusion(result Result) string {
	switch {
	case result.Insufficient:
		return "insufficient_data"
	case result.Significant && result.Direction == "positive":
		return "effective"
	case result.Significant && result.Direction == "negative":
		return "negative"
	default:
		return "no_significant_change"
	}
}

// Experiment is one controlled before/after comparison.
type Experiment struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"projectId"`
	Name              string     `json:"name"`
	Hypothesis        string     `json:"hypothesis"`
	MetricCode        string     `json:"metricCode"`
	BaselineAuditID   *string    `json:"baselineAuditId"`
	TreatmentAuditID  *string    `json:"treatmentAuditId"`
	Status            string     `json:"status"`
	Conclusion        *string    `json:"conclusion"`
	Stats             any        `json:"stats"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// CreateInput declares an experiment before any audit is attached.
type CreateInput struct {
	Name       string `json:"name"`
	Hypothesis string `json:"hypothesis"`
	MetricCode string `json:"metricCode"`
}
