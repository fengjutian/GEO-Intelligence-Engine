package experiments

import (
	"fmt"
	"math"
)

// PlanInput asks how large an experiment must be before it can conclude anything.
type PlanInput struct {
	BaselineRate float64 `json:"baselineRate"`
	MDE          float64 `json:"mde"`
	Alpha        float64 `json:"alpha"`
	Power        float64 `json:"power"`
}

// Plan is the planning answer for one experiment design.
type Plan struct {
	BaselineRate   float64 `json:"baselineRate"`
	MDE            float64 `json:"mde"`
	Alpha          float64 `json:"alpha"`
	Power          float64 `json:"power"`
	PerGroup       int     `json:"perGroup"`
	Total          int     `json:"total"`
	MDEAtPerGroup  float64 `json:"mdeAtPerGroup"`
	Interpretation string  `json:"interpretation"`
}

// DefaultAlpha and DefaultPower match the platform convention.
const (
	DefaultAlpha = 0.05
	DefaultPower = 0.80
)

// zQuantile returns the standard normal quantile for a probability.
func zQuantile(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}
	return math.Sqrt2 * math.Erfinv(2*p-1)
}

// RequiredSampleSize computes the per-group size for a two-proportion test.
func RequiredSampleSize(baselineRate, mde, alpha, power float64) (int, error) {
	if err := validatePlan(baselineRate, mde, alpha, power); err != nil {
		return 0, err
	}
	treatment := baselineRate + mde
	p1q1 := baselineRate * (1 - baselineRate)
	p2q2 := treatment * (1 - treatment)
	zAlpha := zQuantile(1 - alpha/2)
	zPower := zQuantile(power)
	n := (zAlpha + zPower) * (zAlpha + zPower) * (p1q1 + p2q2) / (mde * mde)
	return int(math.Ceil(n)), nil
}

// MinimumDetectableEffect inverts the sample-size formula for a fixed n.
func MinimumDetectableEffect(perGroup int, baselineRate, alpha, power float64) float64 {
	if perGroup <= 0 || baselineRate <= 0 || baselineRate >= 1 {
		return 0
	}
	if alpha <= 0 || alpha >= 1 {
		alpha = DefaultAlpha
	}
	if power <= 0 || power >= 1 {
		power = DefaultPower
	}
	zAlpha := zQuantile(1 - alpha/2)
	zPower := zQuantile(power)
	variance := 2 * baselineRate * (1 - baselineRate)
	return (zAlpha + zPower) * math.Sqrt(variance/float64(perGroup))
}

// AdjustedAlpha returns the O'Brien-Fleming alpha spent at one interim look.
func AdjustedAlpha(look, plannedLooks int, alpha float64) float64 {
	if alpha <= 0 || alpha >= 1 {
		alpha = DefaultAlpha
	}
	if plannedLooks < 1 {
		plannedLooks = 1
	}
	if look < 1 {
		look = 1
	}
	if look > plannedLooks {
		look = plannedLooks
	}
	z := zQuantile(1 - alpha/2)
	adjusted := z / math.Sqrt(float64(look)/float64(plannedLooks))
	return 2 * (1 - normalCDF(adjusted))
}

func normalCDF(value float64) float64 {
	return 0.5 * math.Erfc(-value/math.Sqrt2)
}

// LookDecision is the outcome of one interim analysis.
type LookDecision struct {
	Look          int     `json:"look"`
	PlannedLooks  int     `json:"plannedLooks"`
	AdjustedAlpha float64 `json:"adjustedAlpha"`
	PValue        float64 `json:"pValue"`
	Stop          bool    `json:"stop"`
	Reason        string  `json:"reason"`
}

// EvaluateAtLook applies alpha spending to a batch of results.
func EvaluateAtLook(baseline, treatment Counts, look, plannedLooks int) LookDecision {
	comparison := Compare(baseline, treatment)
	decision := LookDecision{Look: look, PlannedLooks: plannedLooks}
	decision.AdjustedAlpha = AdjustedAlpha(look, plannedLooks, DefaultAlpha)
	decision.PValue = comparison.PValue
	switch {
	case comparison.Insufficient:
		decision.Reason = "insufficient_sample"
	case comparison.PValue <= decision.AdjustedAlpha:
		decision.Stop = true
		decision.Reason = "boundary_crossed"
	default:
		decision.Reason = "continue"
	}
	return decision
}

func validatePlan(baselineRate, mde, alpha, power float64) error {
	if baselineRate <= 0 || baselineRate >= 1 {
		return &PlanError{"baselineRate must be between 0 and 1"}
	}
	if mde <= 0 || baselineRate+mde >= 1 {
		return &PlanError{"mde must be positive and keep the treatment rate below 1"}
	}
	if alpha <= 0 || alpha >= 1 {
		return &PlanError{"alpha must be between 0 and 1"}
	}
	if power <= 0.5 || power >= 1 {
		return &PlanError{"power must be between 0.5 and 1"}
	}
	return nil
}

// PlanError marks an invalid planning request.
type PlanError struct{ Message string }

func (e *PlanError) Error() string { return e.Message }

// BuildPlan fills the derived fields of a planning answer.
func BuildPlan(input PlanInput) (Plan, error) {
	if input.Alpha == 0 {
		input.Alpha = DefaultAlpha
	}
	if input.Power == 0 {
		input.Power = DefaultPower
	}
	perGroup, err := RequiredSampleSize(input.BaselineRate, input.MDE, input.Alpha, input.Power)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{
		BaselineRate:  input.BaselineRate,
		MDE:           input.MDE,
		Alpha:         input.Alpha,
		Power:         input.Power,
		PerGroup:      perGroup,
		Total:         perGroup * 2,
		MDEAtPerGroup: MinimumDetectableEffect(perGroup, input.BaselineRate, input.Alpha, input.Power),
	}
	plan.Interpretation = fmt.Sprintf(
		"至少每组 %d 条样本，才能在 alpha=%.2f 下以 %.2f 功效检出 %.0f 个百分点的绝对提升",
		perGroup, input.Alpha, input.Power, input.MDE*100,
	)
	return plan, nil
}
