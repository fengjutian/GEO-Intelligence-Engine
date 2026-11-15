package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/fengjutian/geo-intelligence-engine/internal/embeddings"
)

type pair struct {
	A          string `json:"a"`
	B          string `json:"b"`
	Same       bool   `json:"same"`
	AnnotatorA *bool  `json:"annotatorA,omitempty"`
	AnnotatorB *bool  `json:"annotatorB,omitempty"`
}

type agreement struct {
	Pairs      int     `json:"pairs"`
	Agreements int     `json:"agreements"`
	Observed   float64 `json:"observed"`
	Expected   float64 `json:"expected"`
	CohenKappa float64 `json:"cohenKappa"`
}

type thresholdResult struct {
	Threshold float64 `json:"threshold"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	TN        int     `json:"tn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

type pairResult struct {
	A          string  `json:"a"`
	B          string  `json:"b"`
	Same       bool    `json:"same"`
	Similarity float64 `json:"similarity"`
}

type report struct {
	Model       string            `json:"model"`
	Pairs       int               `json:"pairs"`
	Best        thresholdResult   `json:"best"`
	Sweep       []thresholdResult `json:"sweep"`
	Agreement   agreement         `json:"agreement"`
	Details     []pairResult      `json:"details"`
	GeneratedAt time.Time         `json:"generatedAt"`
}

func main() {
	dataset := flag.String("dataset", "tests/golden/question-clusters/pairs.json", "labeled pair dataset")
	output := flag.String("output", ".run/cluster-eval.json", "report output")
	flag.Parse()
	raw, err := os.ReadFile(*dataset)
	if err != nil {
		fail(err)
	}
	var pairs []pair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		fail(err)
	}
	client := embeddings.New(os.Getenv("GEO_EMBEDDING_BASE_URL"), os.Getenv("GEO_EMBEDDING_API_KEY"), os.Getenv("GEO_EMBEDDING_MODEL"))
	if !client.Enabled() {
		fail(embeddings.ErrNotConfigured)
	}
	unique := map[string]bool{}
	texts := []string{}
	for _, item := range pairs {
		for _, text := range []string{item.A, item.B} {
			if !unique[text] {
				unique[text] = true
				texts = append(texts, text)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	vectors, err := client.Embed(ctx, texts)
	if err != nil {
		fail(err)
	}
	byText := map[string][]float64{}
	for index, text := range texts {
		byText[text] = vectors[index]
	}
	details := make([]pairResult, 0, len(pairs))
	for _, item := range pairs {
		details = append(details, pairResult{A: item.A, B: item.B, Same: item.Same, Similarity: embeddings.Cosine(byText[item.A], byText[item.B])})
	}
	sweep := []thresholdResult{}
	for threshold := 0.50; threshold <= 0.9501; threshold += 0.05 {
		result := thresholdResult{Threshold: round(threshold)}
		for _, item := range details {
			predicted := item.Similarity >= threshold
			switch {
			case predicted && item.Same:
				result.TP++
			case predicted && !item.Same:
				result.FP++
			case !predicted && item.Same:
				result.FN++
			default:
				result.TN++
			}
		}
		result.Precision = ratio(result.TP, result.TP+result.FP)
		result.Recall = ratio(result.TP, result.TP+result.FN)
		if result.Precision+result.Recall > 0 {
			result.F1 = 2 * result.Precision * result.Recall / (result.Precision + result.Recall)
		}
		sweep = append(sweep, result)
	}
	best := sweep[0]
	for _, item := range sweep[1:] {
		if item.F1 > best.F1 || (item.F1 == best.F1 && item.Precision > best.Precision) {
			best = item
		}
	}
	sort.Slice(details, func(i, j int) bool { return details[i].Similarity > details[j].Similarity })
	reportValue := report{Model: os.Getenv("GEO_EMBEDDING_MODEL"), Pairs: len(pairs), Best: best, Sweep: sweep, Agreement: computeAgreement(pairs), Details: details, GeneratedAt: time.Now().UTC()}
	data, _ := json.MarshalIndent(reportValue, "", "  ")
	if err := os.WriteFile(*output, data, 0o600); err != nil {
		fail(err)
	}
	fmt.Printf("model=%s pairs=%d best_threshold=%.2f precision=%.3f recall=%.3f f1=%.3f\n", reportValue.Model, reportValue.Pairs, best.Threshold, best.Precision, best.Recall, best.F1)
}

func computeAgreement(pairs []pair) agreement {
	result := agreement{Pairs: len(pairs)}
	var aTrue, bTrue int
	for _, item := range pairs {
		a, b := item.Same, item.Same
		if item.AnnotatorA != nil {
			a = *item.AnnotatorA
		}
		if item.AnnotatorB != nil {
			b = *item.AnnotatorB
		}
		if a == b {
			result.Agreements++
		}
		if a {
			aTrue++
		}
		if b {
			bTrue++
		}
	}
	if result.Pairs == 0 {
		return result
	}
	total := float64(result.Pairs)
	result.Observed = float64(result.Agreements) / total
	pA, pB := float64(aTrue)/total, float64(bTrue)/total
	result.Expected = pA*pB + (1-pA)*(1-pB)
	if result.Expected < 1 {
		result.CohenKappa = (result.Observed - result.Expected) / (1 - result.Expected)
	}
	return result
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func round(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
