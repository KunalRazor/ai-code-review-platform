// Command api is the read-only backend for the PR Insights dashboard.
//
// It exposes JSON endpoints the React app calls:
//
//	GET /api/stats     -> headline counts (total PRs, avg risk, total reviews)
//	GET /api/prs       -> recent PRs with their metrics
//	GET /api/hotspots  -> most bug-prone files
//	GET /healthz       -> ok
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/config"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/store"
)

type api struct{ store *store.Store }

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
	a := &api{store: st}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/stats", a.handleStats)
	mux.HandleFunc("/api/prs", a.handlePRs)
	mux.HandleFunc("/api/hotspots", a.handleHotspots)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":" + cfg.APIPort
	log.Printf("insights api listening on %s", addr)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatalf("api error: %v", err)
	}
}

func (a *api) handleStats(w http.ResponseWriter, _ *http.Request) {
	st, err := a.store.Stats()
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, st)
}

func (a *api) handlePRs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	prs, err := a.store.RecentPRs(limit)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, prs)
}

func (a *api) handleHotspots(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := a.store.TopHotspots(r.URL.Query().Get("repo"), limit)
	if err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, rows)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	log.Printf("api error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// withCORS lets the React dev server (a different origin) call this API.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
