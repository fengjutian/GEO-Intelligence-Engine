package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("embedding service is not configured")
var ErrEmbeddingFailed = errors.New("embedding request failed")

// Client calls an OpenAI-compatible /embeddings endpoint.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		Model:   strings.TrimSpace(model),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.BaseURL != "" && c.Model != ""
}

// Embed returns one vector per input text in the same order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if !c.Enabled() {
		return nil, ErrNotConfigured
	}
	if len(texts) == 0 {
		return [][]float64{}, nil
	}
	const batchSize = 10
	vectors := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (c *Client) embedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return [][]float64{}, nil
	}
	body, err := json.Marshal(map[string]any{"model": c.Model, "input": texts})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
	}
	defer response.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d", ErrEmbeddingFailed, response.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("%w: invalid response", ErrEmbeddingFailed)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("%w: expected %d vectors, got %d", ErrEmbeddingFailed, len(texts), len(parsed.Data))
	}
	vectors := make([][]float64, len(parsed.Data))
	for index, item := range parsed.Data {
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("%w: empty vector", ErrEmbeddingFailed)
		}
		vectors[index] = item.Embedding
	}
	return vectors, nil
}
