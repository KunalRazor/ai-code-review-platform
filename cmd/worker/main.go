// Command worker consumes review jobs from Redis and runs them.
//
// It connects to Postgres (store), picks a GitHub provider (real client, or the
// offline fixture when OFFLINE=true), and an analyzer client, then hands each
// job to worker.Processor. Asynq gives us concurrency, retries, and backoff.
package main

import (
	"context"
	"log"

	"github.com/hibiken/asynq"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/analyzer"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/config"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/queue"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/store"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/worker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	st, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	if err := st.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	var gh github.Provider
	if cfg.Offline {
		log.Print("worker in OFFLINE mode: using fixture GitHub provider")
		gh = github.NewFixtureProvider()
	} else {
		gh = github.NewClient(cfg.GitHubToken)
	}

	proc := &worker.Processor{
		Store:         st,
		GitHub:        gh,
		Analyzer:      analyzer.NewClient(cfg.AnalyzerURL),
		MaxDiffBytes:  cfg.MaxDiffBytes,
		MinConfidence: cfg.MinConfidence,
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr},
		asynq.Config{
			Concurrency: 5,
			Queues:      map[string]int{queue.QueueName: 1},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeReviewPR, func(ctx context.Context, t *asynq.Task) error {
		job, err := queue.ParseReviewJob(t)
		if err != nil {
			// Unparseable payload will never succeed; SkipRetry drops it.
			return err
		}
		return proc.Process(ctx, job)
	})

	log.Printf("worker started (redis=%s, analyzer=%s, offline=%v)", cfg.RedisAddr, cfg.AnalyzerURL, cfg.Offline)
	if err := srv.Run(mux); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}
