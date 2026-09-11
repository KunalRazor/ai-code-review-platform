// Package server wires the HTTP webhook to the job queue.
//
// The receiver's only job is to validate the request and hand off work fast:
// GitHub expects a webhook response within ~10s, but a review takes far longer.
// So we verify the signature, enqueue a job, and return 202 immediately. The
// worker (a separate binary) does the slow fetch + analyze + post.
package server

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/queue"
)

const maxBodyBytes = 5 << 20 // 5 MB guard against oversized request bodies.

// WebhookHandler handles incoming GitHub webhook POSTs and enqueues review jobs.
type WebhookHandler struct {
	Secret   string
	Enqueuer queue.EnqueuerAPI
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	// 1. Verify the webhook really came from GitHub (HMAC-SHA256).
	sig := r.Header.Get("X-Hub-Signature-256")
	if !github.VerifySignature(h.Secret, sig, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// 2. We only care about pull_request events.
	if event := r.Header.Get("X-GitHub-Event"); event != "pull_request" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ignoring event: %s", event)
		return
	}

	pr, err := github.ParsePullRequestEvent(body)
	if err != nil {
		http.Error(w, "cannot parse event", http.StatusBadRequest)
		return
	}

	// 3. Only review when a PR is opened, reopened, or gets new commits.
	if pr.Action != "opened" && pr.Action != "synchronize" && pr.Action != "reopened" {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ignoring action: %s", pr.Action)
		return
	}

	// 4. Enqueue and reply fast. The worker takes it from here.
	job := queue.ReviewJob{
		Owner:     pr.Repository.Owner.Login,
		Repo:      pr.Repository.Name,
		Number:    pr.Number,
		CommitSHA: pr.PullRequest.Head.SHA,
		BaseSHA:   pr.PullRequest.Base.SHA,
		Title:     pr.PullRequest.Title,
		Author:    pr.PullRequest.User.Login,
		Action:    pr.Action,
	}
	if err := h.Enqueuer.Enqueue(r.Context(), job); err != nil {
		log.Printf("enqueue failed for %s/%s#%d: %v", job.Owner, job.Repo, job.Number, err)
		http.Error(w, "could not queue review", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, "review queued for %s/%s#%d", job.Owner, job.Repo, job.Number)
}
