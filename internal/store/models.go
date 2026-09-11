// Package store is the PostgreSQL persistence layer (via GORM).
//
// Go owns the operational tables: prs, reviews, review_comments, hotspots.
// The Python analyzer owns the pgvector `file_embeddings` table (created by
// the SQL migration), because embedding + retrieval live on the Python side.
package store

import "time"

// PR is one pull request we have seen. It is upserted on (repo, number): a PR
// gets updated as new commits arrive, so we keep a single row per PR and
// refresh its metrics each time.
type PR struct {
	ID           uint   `gorm:"primaryKey"`
	Repo         string `gorm:"uniqueIndex:idx_pr_repo_number;index"`
	Number       int    `gorm:"uniqueIndex:idx_pr_repo_number"`
	Author       string
	Title        string
	BaseSHA      string
	HeadSHA      string
	LinesAdded   int
	LinesRemoved int
	FilesChanged int
	RiskScore    int
	Status       string // opened | synchronize | reopened | merged | closed
	OpenedAt     time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Review is one AI review run against a specific commit of a PR. The unique
// index on (repo, number, commit_sha) is our idempotency guard: GitHub can
// deliver the same event twice, so we only ever review a given commit once.
type Review struct {
	ID        uint   `gorm:"primaryKey"`
	PRID      uint   `gorm:"index"`
	Repo      string `gorm:"uniqueIndex:idx_review_commit"`
	Number    int    `gorm:"uniqueIndex:idx_review_commit"`
	CommitSHA string `gorm:"uniqueIndex:idx_review_commit"`
	Summary   string
	Model     string
	CreatedAt time.Time
	Comments  []ReviewComment `gorm:"foreignKey:ReviewID"`
}

// ReviewComment is a single inline finding produced by the analyzer.
type ReviewComment struct {
	ID         uint `gorm:"primaryKey"`
	ReviewID   uint `gorm:"index"`
	File       string
	Line       int
	Severity   string // info | low | medium | high
	Confidence float64
	Message    string
	Suggestion string
}

// Hotspot tracks how often a file has appeared in past bug-fix PRs. It feeds
// the risk score: files that break often are riskier to change.
type Hotspot struct {
	ID          uint   `gorm:"primaryKey"`
	Repo        string `gorm:"uniqueIndex:idx_hotspot_repo_file"`
	FilePath    string `gorm:"uniqueIndex:idx_hotspot_repo_file"`
	BugfixCount int
	LastSeen    time.Time
}
