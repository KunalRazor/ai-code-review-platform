package github

import (
	"context"
	"log"
	"strings"
)

// FixtureProvider is an offline stand-in for GitHub. It serves one built-in
// sample PR and "posts" reviews by logging them (and calling OnReview if set),
// so the entire platform can be demoed with no GitHub account and no network.
//
// It satisfies the same Provider interface as the real *Client, so the worker
// code is identical in both modes — only the wiring in main() differs.
type FixtureProvider struct {
	// OnReview, when set, receives the posted review (used by the demo/tests).
	OnReview func(owner, repo string, number int, review ReviewPayload)
}

// NewFixtureProvider returns an offline GitHub provider.
func NewFixtureProvider() *FixtureProvider { return &FixtureProvider{} }

// sampleFile is the "current" content of the file the sample PR changes.
const sampleFile = `package auth

import "errors"

type Session struct {
	UserID string
	Token  *string
}

func Validate(s *Session) error {
	if s == nil {
		return errors.New("nil session")
	}
	return nil
}
`

// samplePatch is the per-file diff hunk for the sample PR. It introduces a
// nil-pointer dereference (*s.Token when Token may be nil) and swallows an
// error, giving the analyzer something real to flag.
const samplePatch = `@@ -9,3 +9,12 @@ func Validate(s *Session) error {
 	if s == nil {
 		return errors.New("nil session")
 	}
+	// BUG: dereferences Token without a nil check.
+	length := len(*s.Token)
+	if length == 0 {
+		return errors.New("empty token")
+	}
+	// swallows the parse error.
+	_ = parseToken(*s.Token)
 	return nil
 }`

// sampleDiff is the full unified diff FetchDiff returns.
const sampleDiff = `diff --git a/internal/auth/token.go b/internal/auth/token.go
index 1111111..2222222 100644
--- a/internal/auth/token.go
+++ b/internal/auth/token.go
` + samplePatch + "\n"

// FetchDiff returns the built-in sample diff.
func (f *FixtureProvider) FetchDiff(_ context.Context, _, _ string, _ int) (string, error) {
	return sampleDiff, nil
}

// ListFiles returns the single changed file in the sample PR.
func (f *FixtureProvider) ListFiles(_ context.Context, _, _ string, _ int) ([]ChangedFile, error) {
	return []ChangedFile{{
		Filename:  "internal/auth/token.go",
		Status:    "modified",
		Additions: 8,
		Deletions: 0,
		Patch:     samplePatch,
	}}, nil
}

// FetchFileContent returns the sample file's content (for RAG context).
func (f *FixtureProvider) FetchFileContent(_ context.Context, _, _, path, _ string) (string, error) {
	if strings.HasSuffix(path, "token.go") {
		return sampleFile, nil
	}
	return "", nil
}

// PostReview logs the review instead of calling GitHub.
func (f *FixtureProvider) PostReview(_ context.Context, owner, repo string, number int, review ReviewPayload) error {
	log.Printf("[offline] would post review on %s/%s#%d: summary=%q, %d inline comment(s)",
		owner, repo, number, truncate(review.Summary, 80), len(review.Comments))
	if f.OnReview != nil {
		f.OnReview(owner, repo, number, review)
	}
	return nil
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
