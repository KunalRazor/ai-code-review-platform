package store

import (
	"fmt"
	"sync"
)

// MemoryStore is an in-memory implementation of the worker's persistence
// needs. It is used by the offline demo (so no database is required) and is
// handy in tests. It is safe for concurrent use.
type MemoryStore struct {
	mu       sync.Mutex
	prSeq    uint
	prs      map[string]*PR   // "repo#number" -> PR
	reviews  map[string]bool  // "repo#number@sha" -> reviewed
	hotspots map[string]int   // "repo|path" -> bugfix count
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		prs:      map[string]*PR{},
		reviews:  map[string]bool{},
		hotspots: map[string]int{},
	}
}

func (m *MemoryStore) AlreadyReviewed(repo string, number int, commitSHA string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reviews[fmt.Sprintf("%s#%d@%s", repo, number, commitSHA)], nil
}

func (m *MemoryStore) HotspotScore(repo string, files []string) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	max := 0
	for _, f := range files {
		if c := m.hotspots[repo+"|"+f]; c > max {
			max = c
		}
	}
	if max == 0 {
		return 0, nil
	}
	return float64(max) / float64(max+5), nil
}

func (m *MemoryStore) UpsertPR(pr *PR) (uint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := fmt.Sprintf("%s#%d", pr.Repo, pr.Number)
	if existing, ok := m.prs[k]; ok {
		pr.ID = existing.ID
	} else {
		m.prSeq++
		pr.ID = m.prSeq
	}
	cp := *pr
	m.prs[k] = &cp
	return pr.ID, nil
}

func (m *MemoryStore) BumpHotspots(repo string, files []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range files {
		m.hotspots[repo+"|"+f]++
	}
	return nil
}

func (m *MemoryStore) SaveReview(r *Review) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reviews[fmt.Sprintf("%s#%d@%s", r.Repo, r.Number, r.CommitSHA)] = true
	return nil
}
