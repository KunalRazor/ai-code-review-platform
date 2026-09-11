package github

import "context"

// ChangedFile is one file touched by a PR, as returned by the GitHub
// "list pull request files" endpoint.
type ChangedFile struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"` // added | modified | removed | renamed
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"` // the per-file diff hunk (may be empty for binaries)
}

// InlineComment is a single line-level review comment to post on a PR.
type InlineComment struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// ReviewPayload is a complete review: one summary + zero or more inline
// comments. The worker builds this from the analyzer's structured output.
type ReviewPayload struct {
	Summary  string
	Comments []InlineComment
	CommitID string // head SHA the review applies to
}

// Provider is everything the worker needs from GitHub. The real *Client talks
// to api.github.com; the FixtureProvider serves local sample data so the whole
// platform runs offline with no GitHub account.
type Provider interface {
	FetchDiff(ctx context.Context, owner, repo string, number int) (string, error)
	ListFiles(ctx context.Context, owner, repo string, number int) ([]ChangedFile, error)
	FetchFileContent(ctx context.Context, owner, repo, path, ref string) (string, error)
	PostReview(ctx context.Context, owner, repo string, number int, review ReviewPayload) error
}
