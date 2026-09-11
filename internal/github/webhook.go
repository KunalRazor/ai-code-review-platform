// Package github handles GitHub webhook parsing/verification and API calls.
package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// PullRequestEvent is the subset of the GitHub "pull_request" webhook payload
// that we actually use. GitHub sends much more; we only decode what we need.
type PullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Title        string `json:"title"`
		HTMLURL      string `json:"html_url"`
		Additions    int    `json:"additions"`
		Deletions    int    `json:"deletions"`
		ChangedFiles int    `json:"changed_files"`
		Head         struct {
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

// VerifySignature checks the X-Hub-Signature-256 header against the raw body
// using HMAC-SHA256 with the shared secret. This proves the request really
// came from GitHub and not an attacker. Uses a constant-time compare.
func VerifySignature(secret, signatureHeader string, body []byte) bool {
	const prefix = "sha256="
	if len(signatureHeader) <= len(prefix) || signatureHeader[:len(prefix)] != prefix {
		return false
	}
	want := signatureHeader[len(prefix):]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(got), []byte(want))
}

// ParsePullRequestEvent decodes the webhook JSON body into a PullRequestEvent.
func ParsePullRequestEvent(body []byte) (*PullRequestEvent, error) {
	var e PullRequestEvent
	if err := json.Unmarshal(body, &e); err != nil {
		return nil, fmt.Errorf("parse pull_request event: %w", err)
	}
	return &e, nil
}
