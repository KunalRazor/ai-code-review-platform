// Package analyzer is the Go client for the Python analyzer service. The
// worker sends it a diff (plus changed files and any related-code context) and
// gets back a structured review: a summary plus line-level comments with
// severity and confidence. Keeping the AI behind an HTTP boundary lets the Go
// side stay fast and lets the Python side own embeddings, RAG, and the LLM.
package analyzer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// File mirrors a changed file sent to the analyzer.
type File struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Request is the analyze request body.
type Request struct {
	Repo          string            `json:"repo"`
	PRNumber      int               `json:"pr_number"`
	Diff          string            `json:"diff"`
	Files         []File            `json:"files"`
	Context       map[string]string `json:"context"` // path -> related file content for RAG
	MinConfidence float64           `json:"min_confidence"`
}

// Comment is one structured finding from the analyzer.
type Comment struct {
	File       string  `json:"file"`
	Line       int     `json:"line"`
	Severity   string  `json:"severity"`
	Confidence float64 `json:"confidence"`
	Message    string  `json:"message"`
	Suggestion string  `json:"suggestion"`
}

// Response is the analyzer's structured review.
type Response struct {
	Summary  string    `json:"summary"`
	Model    string    `json:"model"`
	Comments []Comment `json:"comments"`
}

// Client calls the Python analyzer over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns an analyzer client for the given base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 150 * time.Second},
	}
}

// Analyze POSTs the request to /analyze and returns the structured review.
func (c *Client) Analyze(ctx context.Context, req Request) (*Response, error) {
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call analyzer: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("analyzer status %d: %s", resp.StatusCode, string(body))
	}
	var out Response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode analyzer response: %w", err)
	}
	return &out, nil
}
