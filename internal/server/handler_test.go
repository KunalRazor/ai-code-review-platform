package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/queue"
)

// fakeEnqueuer records jobs instead of talking to Redis.
type fakeEnqueuer struct {
	jobs []queue.ReviewJob
	err  error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, job queue.ReviewJob) error {
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, job)
	return nil
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newRequest(t *testing.T, secret, event string, body []byte, withSig bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", event)
	if withSig {
		req.Header.Set("X-Hub-Signature-256", sign(secret, body))
	}
	return req
}

func TestWebhook_RejectsBadSignature(t *testing.T) {
	h := &WebhookHandler{Secret: "topsecret", Enqueuer: &fakeEnqueuer{}}
	body := []byte(`{"action":"opened"}`)

	req := newRequest(t, "wrong-secret", "pull_request", body, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestWebhook_IgnoresNonPullRequestEvent(t *testing.T) {
	h := &WebhookHandler{Secret: "topsecret", Enqueuer: &fakeEnqueuer{}}
	body := []byte(`{}`)

	req := newRequest(t, "topsecret", "push", body, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ignoring event") {
		t.Fatalf("expected event to be ignored, got %q", rec.Body.String())
	}
}

func TestWebhook_IgnoresUninterestingAction(t *testing.T) {
	h := &WebhookHandler{Secret: "topsecret", Enqueuer: &fakeEnqueuer{}}
	body := []byte(`{"action":"labeled","number":1}`)

	req := newRequest(t, "topsecret", "pull_request", body, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ignoring action") {
		t.Fatalf("expected action to be ignored, got %q", rec.Body.String())
	}
}

func TestWebhook_AcceptsOpenedPRAndEnqueues(t *testing.T) {
	fe := &fakeEnqueuer{}
	h := &WebhookHandler{Secret: "topsecret", Enqueuer: fe}
	body := []byte(`{"action":"opened","number":7,` +
		`"pull_request":{"title":"Add feature","head":{"sha":"abc123"},"base":{"sha":"base1"},"user":{"login":"kunal"}},` +
		`"repository":{"name":"demo","owner":{"login":"me"}}}`)

	req := newRequest(t, "topsecret", "pull_request", body, true)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", rec.Code)
	}
	if len(fe.jobs) != 1 {
		t.Fatalf("want 1 enqueued job, got %d", len(fe.jobs))
	}
	j := fe.jobs[0]
	if j.Owner != "me" || j.Repo != "demo" || j.Number != 7 || j.CommitSHA != "abc123" {
		t.Fatalf("job fields wrong: %+v", j)
	}
}

func TestVerifySignature(t *testing.T) {
	secret := "topsecret"
	body := []byte(`{"hello":"world"}`)

	if !github.VerifySignature(secret, sign(secret, body), body) {
		t.Fatal("valid signature should verify")
	}
	if github.VerifySignature(secret, "sha256=deadbeef", body) {
		t.Fatal("bad signature should not verify")
	}
	if github.VerifySignature(secret, "", body) {
		t.Fatal("empty signature should not verify")
	}
}
