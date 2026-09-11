// Command receiver is the webhook front door.
//
// It verifies GitHub webhook signatures, enqueues a review job on Redis, and
// replies 202 immediately. It does no LLM work itself — that is the worker's
// job — so it can always answer within GitHub's ~10s webhook timeout.
package main

import (
	"log"
	"net/http"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/config"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/queue"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	enq := queue.NewEnqueuer(cfg.RedisAddr)
	defer enq.Close()

	webhook := &server.WebhookHandler{
		Secret:   cfg.WebhookSecret,
		Enqueuer: enq,
	}

	mux := http.NewServeMux()
	mux.Handle("/webhook", webhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":" + cfg.Port
	log.Printf("receiver listening on %s (redis=%s)", addr, cfg.RedisAddr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
