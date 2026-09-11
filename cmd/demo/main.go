// Command demo runs the review pipeline end-to-end offline, with no GitHub
// account, no Redis, and no Postgres. It uses the fixture GitHub provider and
// an in-memory store, and calls the real Python analyzer over HTTP.
//
// Usage:
//
//	# 1. start the analyzer (offline defaults):
//	cd analyzer && . .venv/bin/activate && uvicorn app.main:app --port 9000
//	# 2. run the demo:
//	go run ./cmd/demo
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/analyzer"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/queue"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/store"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/worker"
)

func main() {
	analyzerURL := getenv("ANALYZER_URL", "http://127.0.0.1:9000")

	fixture := github.NewFixtureProvider()
	fixture.OnReview = func(owner, repo string, number int, r github.ReviewPayload) {
		fmt.Printf("\n===== REVIEW POSTED to %s/%s#%d =====\n\n", owner, repo, number)
		fmt.Println(r.Summary)
		for _, c := range r.Comments {
			fmt.Printf("\n── %s:%d ──\n%s\n", c.File, c.Line, c.Body)
		}
		fmt.Println("\n=======================================")
	}

	proc := &worker.Processor{
		Store:         store.NewMemoryStore(),
		GitHub:        fixture,
		Analyzer:      analyzer.NewClient(analyzerURL),
		MaxDiffBytes:  60000,
		MinConfidence: 0.5,
	}

	job := queue.ReviewJob{
		Owner:     "acme",
		Repo:      "widgets",
		Number:    42,
		CommitSHA: "abc1234",
		BaseSHA:   "base000",
		Title:     "Fix token validation",
		Author:    "kunal",
		Action:    "opened",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Printf("offline demo: reviewing sample PR via analyzer at %s", analyzerURL)
	if err := proc.Process(ctx, job); err != nil {
		log.Fatalf("demo failed (is the analyzer running on %s?): %v", analyzerURL, err)
	}
	log.Print("demo complete")
}

func getenv(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
