// Package worker runs a single review job end-to-end. It is deliberately a
// plain struct with a Process method (not tied to Asynq) so it can be unit
// tested with fakes; cmd/worker wires it to the queue.
package worker

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/analyzer"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/insights"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/queue"
	"github.com/kunalsinghrana/ai-code-review-mvp/internal/store"
)

// Store is the persistence the worker needs. Both the GORM *store.Store and
// the in-memory store satisfy it, which keeps the worker testable and lets the
// offline demo run without a database.
type Store interface {
	AlreadyReviewed(repo string, number int, commitSHA string) (bool, error)
	HotspotScore(repo string, files []string) (float64, error)
	UpsertPR(pr *store.PR) (uint, error)
	BumpHotspots(repo string, files []string) error
	SaveReview(r *store.Review) error
}

// Processor holds the dependencies for running a review.
type Processor struct {
	Store         Store
	GitHub        github.Provider
	Analyzer      *analyzer.Client
	MaxDiffBytes  int
	MinConfidence float64
}

// Process runs one review job: idempotency check -> fetch -> insights ->
// analyze -> post -> persist. Returning an error tells Asynq to retry.
func (p *Processor) Process(ctx context.Context, job queue.ReviewJob) error {
	// 1. Idempotency: never review the same commit twice.
	done, err := p.Store.AlreadyReviewed(job.Repo, job.Number, job.CommitSHA)
	if err != nil {
		return err
	}
	if done {
		log.Printf("skip %s/%s#%d@%s: already reviewed", job.Owner, job.Repo, job.Number, short(job.CommitSHA))
		return nil
	}

	// 2. Fetch the changed files (with per-file patches) and the full diff.
	files, err := p.GitHub.ListFiles(ctx, job.Owner, job.Repo, job.Number)
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}
	diff, err := p.GitHub.FetchDiff(ctx, job.Owner, job.Repo, job.Number)
	if err != nil {
		return fmt.Errorf("fetch diff: %w", err)
	}
	if p.MaxDiffBytes > 0 && len(diff) > p.MaxDiffBytes {
		diff = diff[:p.MaxDiffBytes] // cap to stay within token limits
	}

	// 3. Compute PR Insights metrics (size, files, risk).
	paths := filePaths(files)
	hotspot, err := p.Store.HotspotScore(job.Repo, paths)
	if err != nil {
		return err
	}
	m := insights.Compute(files, hotspot)

	// 4. Upsert the PR row with its freshly computed metrics.
	prID, err := p.Store.UpsertPR(&store.PR{
		Repo:         job.Repo,
		Number:       job.Number,
		Author:       job.Author,
		Title:        job.Title,
		BaseSHA:      job.BaseSHA,
		HeadSHA:      job.CommitSHA,
		LinesAdded:   m.LinesAdded,
		LinesRemoved: m.LinesRemoved,
		FilesChanged: m.FilesChanged,
		RiskScore:    m.RiskScore,
		Status:       job.Action,
		OpenedAt:     time.Now().UTC(),
	})
	if err != nil {
		return err
	}

	// 5. If this looks like a bug fix, the files it touches become hotspots.
	if insights.IsBugFix(job.Title) {
		if err := p.Store.BumpHotspots(job.Repo, paths); err != nil {
			log.Printf("bump hotspots failed: %v", err)
		}
	}

	// 6. Gather related-code context for RAG (current content of changed files).
	rctx := map[string]string{}
	for _, f := range files {
		if f.Status == "removed" {
			continue
		}
		content, ferr := p.GitHub.FetchFileContent(ctx, job.Owner, job.Repo, f.Filename, job.CommitSHA)
		if ferr == nil && content != "" {
			rctx[f.Filename] = content
		}
	}

	// 7. Ask the analyzer for a structured review.
	resp, err := p.Analyzer.Analyze(ctx, analyzer.Request{
		Repo:          job.Owner + "/" + job.Repo,
		PRNumber:      job.Number,
		Diff:          diff,
		Files:         toAnalyzerFiles(files),
		Context:       rctx,
		MinConfidence: p.MinConfidence,
	})
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	// 8. Keep only confident comments, then post the review to GitHub.
	inline := make([]github.InlineComment, 0, len(resp.Comments))
	saved := make([]store.ReviewComment, 0, len(resp.Comments))
	for _, c := range resp.Comments {
		if c.Confidence < p.MinConfidence {
			continue
		}
		inline = append(inline, github.InlineComment{
			File: c.File,
			Line: c.Line,
			Body: formatInline(c),
		})
		saved = append(saved, store.ReviewComment{
			File: c.File, Line: c.Line, Severity: c.Severity,
			Confidence: c.Confidence, Message: c.Message, Suggestion: c.Suggestion,
		})
	}

	if err := p.GitHub.PostReview(ctx, job.Owner, job.Repo, job.Number, github.ReviewPayload{
		Summary:  formatSummary(resp.Summary),
		Comments: inline,
		CommitID: job.CommitSHA,
	}); err != nil {
		return fmt.Errorf("post review: %w", err)
	}

	// 9. Persist the review (idempotency guard lives on this table).
	if err := p.Store.SaveReview(&store.Review{
		PRID:      prID,
		Repo:      job.Repo,
		Number:    job.Number,
		CommitSHA: job.CommitSHA,
		Summary:   resp.Summary,
		Model:     resp.Model,
		Comments:  saved,
	}); err != nil {
		return fmt.Errorf("save review: %w", err)
	}

	log.Printf("reviewed %s/%s#%d@%s: risk=%d, %d comment(s), model=%s",
		job.Owner, job.Repo, job.Number, short(job.CommitSHA), m.RiskScore, len(inline), resp.Model)
	return nil
}

func filePaths(files []github.ChangedFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Filename
	}
	return out
}

func toAnalyzerFiles(files []github.ChangedFile) []analyzer.File {
	out := make([]analyzer.File, len(files))
	for i, f := range files {
		out[i] = analyzer.File{
			Filename: f.Filename, Status: f.Status,
			Patch: f.Patch, Additions: f.Additions, Deletions: f.Deletions,
		}
	}
	return out
}

func formatInline(c analyzer.Comment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**[%s]** %s", strings.ToUpper(c.Severity), c.Message)
	if c.Suggestion != "" {
		fmt.Fprintf(&b, "\n\n_Suggestion:_ %s", c.Suggestion)
	}
	fmt.Fprintf(&b, "\n\n<sub>confidence %.0f%%</sub>", c.Confidence*100)
	return b.String()
}

func formatSummary(summary string) string {
	return "## AI Code Review\n\n" + summary +
		"\n\n<sub>Generated automatically. Please use your own judgement.</sub>"
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
