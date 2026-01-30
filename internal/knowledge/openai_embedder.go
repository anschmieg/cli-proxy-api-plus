package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type OpenAIEmbedderOptions struct {
	BaseURL      string
	APIKey       string
	APIKeyHeader string
	APIKeyPrefix string
	Model        string
	Timeout      time.Duration
}

type OpenAIEmbedder struct {
	baseURL      string
	apiKey       string
	apiKeyHeader string
	apiKeyPrefix string
	model        string
	client       *http.Client
}

func NewOpenAIEmbedder(opts OpenAIEmbedderOptions) *OpenAIEmbedder {
	baseURL := strings.TrimSpace(opts.BaseURL)
	apiKey := strings.TrimSpace(opts.APIKey)
	model := strings.TrimSpace(opts.Model)
	if baseURL == "" || apiKey == "" || model == "" {
		return nil
	}
	header := strings.TrimSpace(opts.APIKeyHeader)
	if header == "" {
		header = "Authorization"
	}
	prefix := strings.TrimSpace(opts.APIKeyPrefix)
	if prefix == "" {
		prefix = "Bearer"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	return &OpenAIEmbedder{
		baseURL:      strings.TrimRight(baseURL, "/"),
		apiKey:       apiKey,
		apiKeyHeader: header,
		apiKeyPrefix: prefix,
		model:        model,
		client:       &http.Client{Timeout: timeout},
	}
}

func (e *OpenAIEmbedder) GetModel() string {
	if e == nil {
		return ""
	}
	return e.model
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("embedder not configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	payload := struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}{
		Input: texts,
		Model: e.model,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	keyValue := e.apiKey
	if e.apiKeyPrefix != "" {
		keyValue = e.apiKeyPrefix + " " + e.apiKey
	}
	req.Header.Set(e.apiKeyHeader, keyValue)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embeddings request failed: %s", resp.Status)
	}

	var decoded struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}

	out := make([][]float32, len(decoded.Data))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(out) {
			continue
		}
		vec := make([]float32, len(item.Embedding))
		for i, value := range item.Embedding {
			vec[i] = float32(value)
		}
		out[item.Index] = vec
	}
	return out, nil
}
