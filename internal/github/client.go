package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const apiBase = "https://api.github.com"

// Client is a minimal GitHub REST API client using the standard library.
type Client struct {
	token string
	http  *http.Client
}

// NewClient returns a GitHub client authenticated with the given token.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchDiff returns the unified diff for a pull request. We ask GitHub for the
// diff media type so we get the raw ".diff" text instead of JSON.
func (c *Client) FetchDiff(ctx context.Context, owner, repo string, number int) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", apiBase, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github fetch diff: status %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

// PostComment posts a comment on the PR conversation. GitHub treats PRs as
// issues for the comments API, so we use the /issues/{n}/comments endpoint.
func (c *Client) PostComment(ctx context.Context, owner, repo string, number int, comment string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", apiBase, owner, repo, number)
	payload, err := json.Marshal(map[string]string{"body": comment})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github post comment: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// ListFiles returns the files changed by a PR (with per-file patch hunks).
// We request up to 100 files (one page); very large PRs would need pagination,
// which we intentionally skip to keep the review focused and cheap.
func (c *Client) ListFiles(ctx context.Context, owner, repo string, number int) ([]ChangedFile, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100", apiBase, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github list files: status %d: %s", resp.StatusCode, string(body))
	}
	var files []ChangedFile
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, fmt.Errorf("decode files: %w", err)
	}
	return files, nil
}

// FetchFileContent returns the raw content of a file at a given ref. Used to
// pull in related code (RAG context) beyond the diff itself.
func (c *Client) FetchFileContent(ctx context.Context, owner, repo, path, ref string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s", apiBase, owner, repo, path)
	if ref != "" {
		url += "?ref=" + ref
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	c.setHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.raw")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github file content %s: status %d", path, resp.StatusCode)
	}
	return string(body), nil
}

// PostReview posts a single PR review: a summary body plus inline comments
// anchored to specific lines. event=COMMENT means "leave feedback without
// approving or requesting changes".
func (c *Client) PostReview(ctx context.Context, owner, repo string, number int, review ReviewPayload) error {
	type ghComment struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Side string `json:"side"`
		Body string `json:"body"`
	}
	comments := make([]ghComment, 0, len(review.Comments))
	for _, ic := range review.Comments {
		comments = append(comments, ghComment{Path: ic.File, Line: ic.Line, Side: "RIGHT", Body: ic.Body})
	}
	payload := map[string]any{
		"body":     review.Summary,
		"event":    "COMMENT",
		"comments": comments,
	}
	if review.CommitID != "" {
		payload["commit_id"] = review.CommitID
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", apiBase, owner, repo, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		// Inline comments fail if a line is not part of the diff. Fall back to a
		// plain summary comment so the review is never silently lost.
		if len(comments) > 0 {
			return c.PostComment(ctx, owner, repo, number, review.Summary)
		}
		return fmt.Errorf("github post review: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "ai-code-review-platform")
}
