// Package queue is the async job boundary between the receiver and the worker.
//
// It uses Asynq (Redis-backed). The receiver enqueues a ReviewJob and returns
// immediately; the worker consumes jobs at its own pace. This is what lets the
// webhook reply in milliseconds while the (slow) LLM review happens in the
// background, and it gives us retries + backoff + a dead-letter queue for free.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// TypeReviewPR is the Asynq task type for "review this PR".
const TypeReviewPR = "review:pr"

// QueueName is the named Asynq queue we use.
const QueueName = "reviews"

// ReviewJob is the payload carried from receiver to worker.
type ReviewJob struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Number    int    `json:"number"`
	CommitSHA string `json:"commit_sha"`
	BaseSHA   string `json:"base_sha"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Action    string `json:"action"`
}

// dedupeKey uniquely identifies a review of one commit. Enqueuing the same
// commit twice collapses to a single task, so duplicate webhook deliveries do
// not cause duplicate reviews.
func (j ReviewJob) dedupeKey() string {
	return fmt.Sprintf("review:%s/%s#%d@%s", j.Owner, j.Repo, j.Number, j.CommitSHA)
}

// Enqueuer publishes review jobs onto the queue.
type Enqueuer struct {
	client *asynq.Client
}

// NewEnqueuer connects to Redis and returns an Enqueuer.
func NewEnqueuer(redisAddr string) *Enqueuer {
	return &Enqueuer{client: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})}
}

// Enqueue puts a ReviewJob on the queue. Duplicate commits (same dedupe key)
// are treated as success, not an error.
func (e *Enqueuer) Enqueue(ctx context.Context, job ReviewJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal review job: %w", err)
	}
	task := asynq.NewTask(TypeReviewPR, payload)
	_, err = e.client.EnqueueContext(ctx, task,
		asynq.Queue(QueueName),
		asynq.TaskID(job.dedupeKey()),
		asynq.MaxRetry(3),
		asynq.Timeout(4*time.Minute),
		asynq.Retention(24*time.Hour),
	)
	// A duplicate enqueue for the same commit is a no-op, not a failure.
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("enqueue review job: %w", err)
	}
	return nil
}

// Close releases the Redis client.
func (e *Enqueuer) Close() error { return e.client.Close() }

// ParseReviewJob decodes a task payload back into a ReviewJob (worker side).
func ParseReviewJob(t *asynq.Task) (ReviewJob, error) {
	var job ReviewJob
	if err := json.Unmarshal(t.Payload(), &job); err != nil {
		return job, fmt.Errorf("parse review job: %w", err)
	}
	return job, nil
}

// EnqueuerAPI is the behaviour the receiver depends on (satisfied by *Enqueuer).
// Defining it here keeps the receiver testable with a fake.
type EnqueuerAPI interface {
	Enqueue(ctx context.Context, job ReviewJob) error
}
