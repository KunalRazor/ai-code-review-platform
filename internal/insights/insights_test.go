package insights

import (
	"testing"

	"github.com/kunalsinghrana/ai-code-review-mvp/internal/github"
)

func TestCompute_CountsAndTestDetection(t *testing.T) {
	files := []github.ChangedFile{
		{Filename: "internal/auth/token.go", Additions: 20, Deletions: 5},
		{Filename: "internal/auth/token_test.go", Additions: 30, Deletions: 0},
	}
	m := Compute(files, 0)

	if m.LinesAdded != 50 || m.LinesRemoved != 5 {
		t.Fatalf("wrong line counts: %+v", m)
	}
	if m.FilesChanged != 2 {
		t.Fatalf("want 2 files, got %d", m.FilesChanged)
	}
	if !m.TestFileTouched {
		t.Fatal("expected test file to be detected")
	}
}

func TestCompute_RiskInRange(t *testing.T) {
	files := []github.ChangedFile{{Filename: "a.go", Additions: 1000, Deletions: 500}}
	m := Compute(files, 1.0)
	if m.RiskScore < 0 || m.RiskScore > 100 {
		t.Fatalf("risk out of range: %d", m.RiskScore)
	}
}

func TestCompute_TestsLowerRisk(t *testing.T) {
	code := []github.ChangedFile{{Filename: "svc.go", Additions: 100, Deletions: 20}}
	withTests := []github.ChangedFile{
		{Filename: "svc.go", Additions: 100, Deletions: 20},
		{Filename: "svc_test.go", Additions: 40, Deletions: 0},
	}
	// Same hotspot score; touching tests should not increase risk beyond the
	// no-tests case for the equivalent code change.
	riskNoTests := Compute(code, 0).RiskScore
	riskWithTests := Compute(withTests, 0).RiskScore
	if riskWithTests > riskNoTests {
		t.Fatalf("touching tests should not raise risk: withTests=%d noTests=%d", riskWithTests, riskNoTests)
	}
}

func TestIsBugFix(t *testing.T) {
	cases := map[string]bool{
		"Fix nil pointer in auth":     true,
		"hotfix: payment timeout":     true,
		"Add new dashboard widget":    false,
		"Refactor config loading":     false,
		"bugfix for regression #123":  true,
	}
	for title, want := range cases {
		if got := IsBugFix(title); got != want {
			t.Errorf("IsBugFix(%q)=%v want %v", title, got, want)
		}
	}
}
