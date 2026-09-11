"""Tests for the analyzer. Run with:  cd analyzer && python -m pytest

They run fully offline (hash embedder + mock reviewer, no DB, no API keys).
"""

from __future__ import annotations

from fastapi.testclient import TestClient

from app.graph import Pipeline
from app.llm import MockReviewer
from app.patch import added_lines
from app.schemas import AnalyzeRequest, FileIn
from app.settings import Settings

SAMPLE_PATCH = """@@ -9,3 +9,12 @@ func Validate(s *Session) error {
 	if s == nil {
 		return errors.New("nil session")
 	}
+	length := len(*s.Token)
+	if length == 0 {
+		return errors.New("empty token")
+	}
+	_ = parseToken(*s.Token)
 	return nil
 }"""


def _settings() -> Settings:
    return Settings(
        embeddings_provider="hash", llm_provider="mock", embedding_dim=128,
        database_url=None, anthropic_api_key=None, openai_api_key=None,
        llm_model="mock", rag_top_k=3,
    )


def test_added_lines_have_correct_new_file_numbers():
    lines = added_lines(SAMPLE_PATCH)
    texts = [l.text.strip() for l in lines]
    assert any("len(*s.Token)" in t for t in texts)
    # First added line sits at new-file line 12 (hunk starts at +9, 3 context lines).
    assert lines[0].line_no == 12


def test_mock_reviewer_flags_nil_deref_and_ignored_error():
    result = MockReviewer().review("", [FileIn(filename="auth.go", patch=SAMPLE_PATCH)], [])
    messages = " ".join(c.message.lower() for c in result.comments)
    assert "dereferenced" in messages       # nil pointer rule
    assert "discarded" in messages          # ignored error rule
    assert all(c.line > 0 for c in result.comments)


def test_pipeline_end_to_end_offline():
    pipe = Pipeline(_settings())
    req = AnalyzeRequest(
        repo="me/demo", pr_number=1, diff="```" + SAMPLE_PATCH + "```",
        files=[FileIn(filename="internal/auth/token.go", patch=SAMPLE_PATCH, additions=5)],
        context={"internal/auth/token.go": "package auth\nfunc Validate(s *Session) error { return nil }\n"},
        min_confidence=0.5,
    )
    resp = pipe.run(req)
    assert resp.model == "mock-heuristic"
    assert len(resp.comments) >= 1
    assert "context" in resp.summary.lower()  # RAG stage ran


def test_confidence_threshold_filters_low_signal():
    pipe = Pipeline(_settings())
    req = AnalyzeRequest(
        repo="me/demo", files=[FileIn(filename="a.go", patch=SAMPLE_PATCH)],
        min_confidence=0.99,  # nothing should clear this
    )
    resp = pipe.run(req)
    assert resp.comments == []


def test_analyze_endpoint():
    from app.main import app

    client = TestClient(app)
    r = client.post("/analyze", json={
        "repo": "me/demo", "pr_number": 2, "diff": "",
        "files": [{"filename": "auth.go", "patch": SAMPLE_PATCH}],
        "context": {}, "min_confidence": 0.5,
    })
    assert r.status_code == 200
    body = r.json()
    assert "summary" in body and "comments" in body
    assert body["model"] == "mock-heuristic"
