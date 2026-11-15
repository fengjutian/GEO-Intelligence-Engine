package semantic

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Verdicts a check can carry.
const (
	VerdictSupported   = "supported"
	VerdictUnsupported = "unsupported"
	VerdictUnknown     = "unknown"
)

// Claim is one assertion extracted from a model answer.
type Claim struct {
	AnswerIndex int    `json:"answerIndex"`
	Text        string `json:"text"`
}

// Evidence is one passage the claim is checked against.
type Evidence struct {
	Source string  `json:"source"`
	Ref    string  `json:"ref"`
	Text   string  `json:"text"`
	Score  float64 `json:"score"`
}

// Check is one stored verdict.
type Check struct {
	ID             string  `json:"id"`
	Claim          string  `json:"claim"`
	Verdict        string  `json:"verdict"`
	Confidence     float64 `json:"confidence"`
	Similarity     float64 `json:"similarity"`
	EvidenceSource string  `json:"evidenceSource"`
	EvidenceText   string  `json:"evidenceText"`
	Status         string  `json:"status"`
}

// Run is one verification pass over an audit.
type Run struct {
	ID         string    `json:"id"`
	AuditID    string    `json:"auditId"`
	Status     string    `json:"status"`
	Model      string    `json:"model"`
	ClaimCount int       `json:"claimCount"`
	Checks     []Check   `json:"checks"`
	CreatedAt  time.Time `json:"createdAt"`
}

// MaxClaims bounds one run so a single audit cannot trigger unbounded model calls.
const MaxClaims = 8

// MaxEvidencePerClaim bounds the passages handed to the judge.
const MaxEvidencePerClaim = 3

// EvidenceThreshold is the minimum retrieval score before a judge call is made.
const EvidenceThreshold = 0.35

// SplitClaims turns answer text into candidate factual assertions.
func SplitClaims(text string) []string {
	replaced := strings.NewReplacer("。", ".", "！", ".", "？", ".", "\n", ".").Replace(text)
	seen := map[string]bool{}
	claims := []string{}
	for _, part := range strings.Split(replaced, ".") {
		claim := strings.TrimSpace(part)
		claim = strings.Trim(claim, "-*#> 　")
		if !isVerifiableClaim(claim) {
			continue
		}
		key := strings.ToLower(claim)
		if seen[key] {
			continue
		}
		seen[key] = true
		claims = append(claims, claim)
		if len(claims) >= MaxClaims {
			break
		}
	}
	return claims
}

// isVerifiableClaim rejects markdown table rows and fragments that carry no
// assertable statement, which otherwise dominate model answers with tables.
func isVerifiableClaim(claim string) bool {
	length := len([]rune(claim))
	if length < 10 || length > 240 {
		return false
	}
	if strings.Count(claim, "|") > 1 || strings.Count(claim, "*") > 2 {
		return false
	}
	letters := 0
	for _, value := range claim {
		if unicode.IsLetter(value) || unicode.Is(unicode.Han, value) || unicode.IsDigit(value) {
			letters++
		}
	}
	if letters*2 < length {
		return false
	}
	return true
}

// Terms tokenizes mixed Chinese/English text for lexical retrieval.
func Terms(text string) map[string]bool {
	terms := map[string]bool{}
	word := strings.Builder{}
	flush := func() {
		if word.Len() >= 2 {
			terms[strings.ToLower(word.String())] = true
		}
		word.Reset()
	}
	runes := []rune(strings.ToLower(text))
	for index, value := range runes {
		switch {
		case unicode.Is(unicode.Han, value):
			flush()
			if index+1 < len(runes) && unicode.Is(unicode.Han, runes[index+1]) {
				terms[string(runes[index:index+2])] = true
			}
		case unicode.IsLetter(value) || unicode.IsDigit(value):
			word.WriteRune(value)
		default:
			flush()
		}
	}
	flush()
	return terms
}

// LexicalScore is a Jaccard overlap used when embeddings are unavailable.
func LexicalScore(claim, evidence string) float64 {
	left := Terms(claim)
	right := Terms(evidence)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	intersection := 0
	for term := range left {
		if right[term] {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	return float64(intersection) / float64(union)
}

// Cosine is the embedding similarity, guards zero vectors.
func Cosine(left, right []float64) float64 {
	if len(left) == 0 || len(left) != len(right) {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index := range left {
		dot += left[index] * right[index]
		leftNorm += left[index] * left[index]
		rightNorm += right[index] * right[index]
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / (math.Sqrt(leftNorm) * math.Sqrt(rightNorm))
}

// RankEvidence returns the best passages for a claim, highest score first.
func RankEvidence(claim string, pool []Evidence, scores []float64, limit int) []Evidence {
	ranked := make([]Evidence, len(pool))
	copy(ranked, pool)
	for index := range ranked {
		if index < len(scores) {
			ranked[index].Score = scores[index]
		} else {
			ranked[index].Score = LexicalScore(claim, ranked[index].Text)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// NeedsJudgement reports whether retrieval found enough signal to ask a model.
func NeedsJudgement(top Evidence) bool {
	return top.Score >= EvidenceThreshold
}

// NormalizeVerdict maps model output onto the three allowed verdicts.
func NormalizeVerdict(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case VerdictSupported, "true", "yes", "supported.":
		return VerdictSupported
	case VerdictUnsupported, "false", "no", "contradicted", "unsupported.":
		return VerdictUnsupported
	default:
		return VerdictUnknown
	}
}
