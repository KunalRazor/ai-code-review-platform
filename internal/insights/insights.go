// Package insights turns raw PR data into product metrics: size, files
// touched, whether tests changed, and a 0..100 risk score. These power the
// PR Insights dashboard and can gate risky merges.
package insights

import (
	"math"
	"strings"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
)

// Metrics is the computed insight for one PR.
type Metrics struct {
	LinesAdded      int
	LinesRemoved    int
	FilesChanged    int
	TestFileTouched bool
	RiskScore       int // 0..100
}

// Weights for the risk score. They sum to 1.0 so the score stays in 0..100.
const (
	wSize    = 0.35 // bigger diffs are riskier
	wFiles   = 0.25 // touching many files is riskier
	wHotspot = 0.25 // touching historically bug-prone files is riskier
	wNoTests = 0.15 // changing code without touching tests is riskier
)

// Compute builds metrics from the changed files plus a hotspot score (0..1,
// supplied by the store from historical bug-fix frequency).
//
// Each raw count is squashed into 0..1 with diminishing returns so one huge PR
// cannot dominate the score.
func Compute(files []github.ChangedFile, hotspotScore float64) Metrics {
	var added, removed int
	testTouched := false
	for _, f := range files {
		added += f.Additions
		removed += f.Deletions
		if isTestFile(f.Filename) {
			testTouched = true
		}
	}
	size := added + removed

	sizeScore := diminishing(float64(size), 400)          // 400 changed lines -> 0.5
	filesScore := diminishing(float64(len(files)), 10)    // 10 files -> 0.5
	noTests := 0.0                                         // touching tests reduces risk
	if !testTouched {
		noTests = 1.0
	}

	risk := 100 * (wSize*sizeScore + wFiles*filesScore + wHotspot*clamp01(hotspotScore) + wNoTests*noTests)

	return Metrics{
		LinesAdded:      added,
		LinesRemoved:    removed,
		FilesChanged:    len(files),
		TestFileTouched: testTouched,
		RiskScore:       clampInt(int(math.Round(risk)), 0, 100),
	}
}

// IsBugFix guesses whether a PR is a bug fix from its title, so the files it
// touches can have their hotspot counters bumped.
func IsBugFix(title string) bool {
	t := strings.ToLower(title)
	for _, kw := range []string{"fix", "bug", "hotfix", "patch", "regression"} {
		if strings.Contains(t, kw) {
			return true
		}
	}
	return false
}

// diminishing maps x>=0 to 0..1 with x==half giving 0.5.
func diminishing(x, half float64) float64 {
	if x <= 0 {
		return 0
	}
	return x / (x + half)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func clampInt(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func isTestFile(path string) bool {
	p := strings.ToLower(path)
	return strings.HasSuffix(p, "_test.go") ||
		strings.Contains(p, ".test.") ||
		strings.Contains(p, ".spec.") ||
		strings.Contains(p, "/tests/") ||
		strings.Contains(p, "/test/") ||
		strings.HasPrefix(p, "test_") ||
		strings.Contains(p, "/test_")
}
