package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// Store is a thin repository over GORM.
type Store struct {
	db *gorm.DB
}

// Open connects to PostgreSQL using the given DSN.
func Open(dsn string) (*Store, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	return &Store{db: db}, nil
}

// Migrate creates/updates the tables Go owns. The pgvector extension and the
// file_embeddings table are created by the SQL migration (Python owns those).
func (s *Store) Migrate() error {
	return s.db.AutoMigrate(&PR{}, &Review{}, &ReviewComment{}, &Hotspot{})
}

// UpsertPR inserts or updates a PR row keyed by (repo, number) and returns the
// row id so reviews can reference it.
func (s *Store) UpsertPR(pr *PR) (uint, error) {
	err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "repo"}, {Name: "number"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"author", "title", "base_sha", "head_sha", "lines_added",
			"lines_removed", "files_changed", "risk_score", "status", "updated_at",
		}),
	}).Create(pr).Error
	if err != nil {
		return 0, fmt.Errorf("upsert pr: %w", err)
	}
	return pr.ID, nil
}

// AlreadyReviewed reports whether we have already reviewed this exact commit.
// This is the idempotency check that makes retries and duplicate webhook
// deliveries safe.
func (s *Store) AlreadyReviewed(repo string, number int, commitSHA string) (bool, error) {
	var count int64
	err := s.db.Model(&Review{}).
		Where("repo = ? AND number = ? AND commit_sha = ?", repo, number, commitSHA).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check reviewed: %w", err)
	}
	return count > 0, nil
}

// SaveReview persists a review and its inline comments in one transaction.
func (s *Store) SaveReview(r *Review) error {
	if err := s.db.Create(r).Error; err != nil {
		return fmt.Errorf("save review: %w", err)
	}
	return nil
}

// HotspotScore returns the highest bugfix count among the given files, used as
// a 0..1 signal in the risk score. Unknown files contribute 0.
func (s *Store) HotspotScore(repo string, files []string) (float64, error) {
	if len(files) == 0 {
		return 0, nil
	}
	var max int
	err := s.db.Model(&Hotspot{}).
		Where("repo = ? AND file_path IN ?", repo, files).
		Select("COALESCE(MAX(bugfix_count), 0)").
		Scan(&max).Error
	if err != nil {
		return 0, fmt.Errorf("hotspot score: %w", err)
	}
	// Squash the raw count into 0..1 with diminishing returns.
	return float64(max) / float64(max+5), nil
}

// BumpHotspots increments the bugfix counter for each file (used when a PR
// looks like a bug fix). Upserts so new files start at 1.
func (s *Store) BumpHotspots(repo string, files []string) error {
	now := time.Now().UTC()
	for _, f := range files {
		h := Hotspot{Repo: repo, FilePath: f, BugfixCount: 1, LastSeen: now}
		err := s.db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "repo"}, {Name: "file_path"}},
			DoUpdates: clause.Assignments(map[string]any{
				"bugfix_count": gorm.Expr("hotspots.bugfix_count + 1"),
				"last_seen":    now,
			}),
		}).Create(&h).Error
		if err != nil {
			return fmt.Errorf("bump hotspot %s: %w", f, err)
		}
	}
	return nil
}

// ---- Read side (dashboard API) ----

// PRSummary is a compact row for the dashboard list.
type PRSummary struct {
	Repo         string    `json:"repo"`
	Number       int       `json:"number"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	LinesAdded   int       `json:"lines_added"`
	LinesRemoved int       `json:"lines_removed"`
	FilesChanged int       `json:"files_changed"`
	RiskScore    int       `json:"risk_score"`
	Status       string    `json:"status"`
	OpenedAt     time.Time `json:"opened_at"`
}

// RecentPRs returns the most recent PRs for the dashboard.
func (s *Store) RecentPRs(limit int) ([]PRSummary, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []PRSummary
	err := s.db.Model(&PR{}).
		Order("opened_at DESC").
		Limit(limit).
		Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("recent prs: %w", err)
	}
	return out, nil
}

// HotspotRow is a hotspot for the dashboard table.
type HotspotRow struct {
	FilePath    string    `json:"file_path"`
	BugfixCount int       `json:"bugfix_count"`
	LastSeen    time.Time `json:"last_seen"`
}

// TopHotspots returns the most bug-prone files.
func (s *Store) TopHotspots(repo string, limit int) ([]HotspotRow, error) {
	if limit <= 0 {
		limit = 10
	}
	q := s.db.Model(&Hotspot{}).Order("bugfix_count DESC").Limit(limit)
	if repo != "" {
		q = q.Where("repo = ?", repo)
	}
	var out []HotspotRow
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("top hotspots: %w", err)
	}
	return out, nil
}

// Stats holds headline numbers for the dashboard.
type Stats struct {
	TotalPRs     int64   `json:"total_prs"`
	AvgRiskScore float64 `json:"avg_risk_score"`
	TotalReviews int64   `json:"total_reviews"`
}

// Stats returns aggregate counts for the dashboard header.
func (s *Store) Stats() (Stats, error) {
	var st Stats
	if err := s.db.Model(&PR{}).Count(&st.TotalPRs).Error; err != nil {
		return st, err
	}
	if err := s.db.Model(&Review{}).Count(&st.TotalReviews).Error; err != nil {
		return st, err
	}
	// AVG over an empty table returns NULL; Scan into a nullable then default.
	var avg *float64
	if err := s.db.Model(&PR{}).Select("AVG(risk_score)").Scan(&avg).Error; err != nil {
		return st, err
	}
	if avg != nil {
		st.AvgRiskScore = *avg
	}
	return st, nil
}

// ErrNotFound is returned when a lookup finds nothing.
var ErrNotFound = errors.New("not found")
