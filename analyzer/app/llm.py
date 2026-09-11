"""Reviewer providers: turn a diff (+ retrieved context) into findings.

MockReviewer is a deterministic, offline heuristic reviewer: it scans added
lines for common issues (hardcoded secrets, ignored errors, nil dereferences,
panics, debug prints, TODOs) and returns real, line-anchored findings — so the
whole platform demos without any API key. AnthropicReviewer / OpenAIReviewer
call a real LLM and force structured JSON output when a key is configured.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from typing import Protocol

from .patch import added_lines
from .schemas import FileIn
from .settings import Settings


@dataclass
class Finding:
    file: str
    line: int
    severity: str
    confidence: float
    message: str
    suggestion: str = ""


@dataclass
class ReviewResult:
    summary: str
    model: str
    comments: list[Finding] = field(default_factory=list)


class Reviewer(Protocol):
    def review(self, diff: str, files: list[FileIn], context_snippets: list[str]) -> ReviewResult: ...


# (compiled regex, severity, confidence, message, suggestion)
_RULES: list[tuple[re.Pattern[str], str, float, str, str]] = [
    (
        re.compile(r"""(?i)(password|passwd|secret|api[_-]?key|apikey|token)\s*[:=]\s*["'][^"']{3,}["']"""),
        "high", 0.85,
        "Possible hardcoded secret/credential.",
        "Load it from an environment variable or secret store, not source code.",
    ),
    (
        re.compile(r"^\s*_(\s*,\s*_)*\s*[:=]{1,2}\s*\w[\w.]*\("),
        "medium", 0.6,
        "Return value (possibly an error) is discarded with `_`.",
        "Handle or log the returned error instead of ignoring it.",
    ),
    (
        re.compile(r"(len\(\*|=\s*\*[A-Za-z_]|return\s+\*[A-Za-z_])"),
        "medium", 0.55,
        "Pointer is dereferenced without an obvious nil check.",
        "Guard against nil before dereferencing.",
    ),
    (
        re.compile(r"\bpanic\("),
        "medium", 0.6,
        "panic() in non-main code path.",
        "Return an error to the caller instead of panicking.",
    ),
    (
        re.compile(r"(fmt\.Print|console\.log|System\.out\.print|\bprintln!|(?<![A-Za-z_])print\()"),
        "low", 0.5,
        "Debug print statement left in the code.",
        "Remove it or switch to the structured logger.",
    ),
    (
        re.compile(r"(?i)\b(todo|fixme)\b"),
        "info", 0.9,
        "Unresolved TODO/FIXME in changed code.",
        "Resolve it or link a tracking issue before merging.",
    ),
    (
        re.compile(r"except\s*:|except\s+Exception\b"),
        "medium", 0.6,
        "Overly broad exception handler.",
        "Catch the specific exception type you expect.",
    ),
    (
        re.compile(r"[=!]=\s*None\b"),
        "low", 0.5,
        "Comparing to None with ==/!=.",
        "Use `is None` / `is not None`.",
    ),
]


class MockReviewer:
    model = "mock-heuristic"

    def review(self, diff: str, files: list[FileIn], context_snippets: list[str]) -> ReviewResult:
        findings: list[Finding] = []
        for f in files:
            if not f.patch:
                continue
            for al in added_lines(f.patch):
                findings.extend(self._scan(f.filename, al.line_no, al.text))
        summary = self._summarize(files, findings, len(context_snippets))
        return ReviewResult(summary=summary, model=self.model, comments=findings)

    def _scan(self, path: str, line: int, text: str) -> list[Finding]:
        out: list[Finding] = []
        for pattern, severity, conf, msg, suggestion in _RULES:
            if pattern.search(text):
                out.append(Finding(
                    file=path, line=line, severity=severity, confidence=conf,
                    message=msg, suggestion=suggestion,
                ))
        return out

    def _summarize(self, files: list[FileIn], findings: list[Finding], ctx: int) -> str:
        by_sev: dict[str, int] = {}
        for fnd in findings:
            by_sev[fnd.severity] = by_sev.get(fnd.severity, 0) + 1
        n_files = len(files)
        if not findings:
            head = f"Reviewed {n_files} file(s); no issues found by the heuristic reviewer."
        else:
            parts = ", ".join(f"{c} {s}" for s, c in sorted(by_sev.items()))
            head = f"Reviewed {n_files} file(s) and found {len(findings)} issue(s): {parts}."
        return f"{head}\n\n_Considered {ctx} related code snippet(s) for context (RAG)._"


class _LLMReviewer:
    """Shared logic for LLM-backed reviewers: build a prompt that demands
    strict JSON, then parse it into findings. Falls back to a summary-only
    result if the model does not return valid JSON."""

    model = "llm"

    _SYSTEM = (
        "You are a senior engineer doing a code review. You are given a unified "
        "diff and some related code for context. Return ONLY JSON of the form:\n"
        '{"summary": string, "comments": [{"file": string, "line": number, '
        '"severity": "info|low|medium|high", "confidence": number 0..1, '
        '"message": string, "suggestion": string}]}\n'
        "Only report real issues. line must be a line added in the diff."
    )

    def _prompt(self, diff: str, context_snippets: list[str]) -> str:
        ctx = "\n\n".join(context_snippets[:5])
        return (
            f"## Related code (context)\n```\n{ctx}\n```\n\n"
            f"## Diff to review\n```diff\n{diff}\n```\n\n"
            "Respond with the JSON described in the system prompt."
        )

    def _parse(self, raw: str) -> ReviewResult:
        text = raw.strip()
        start, end = text.find("{"), text.rfind("}")
        if start == -1 or end == -1:
            return ReviewResult(summary=text[:500] or "No structured output.", model=self.model)
        try:
            data = json.loads(text[start:end + 1])
        except json.JSONDecodeError:
            return ReviewResult(summary="Model returned invalid JSON.", model=self.model)
        comments = [
            Finding(
                file=c.get("file", ""), line=int(c.get("line", 0)),
                severity=c.get("severity", "info"), confidence=float(c.get("confidence", 0.5)),
                message=c.get("message", ""), suggestion=c.get("suggestion", ""),
            )
            for c in data.get("comments", [])
        ]
        return ReviewResult(summary=data.get("summary", ""), model=self.model, comments=comments)


class AnthropicReviewer(_LLMReviewer):
    def __init__(self, settings: Settings) -> None:
        if not settings.anthropic_api_key:
            raise ValueError("ANTHROPIC_API_KEY required for anthropic provider")
        self._key = settings.anthropic_api_key
        self.model = settings.llm_model

    def review(self, diff: str, files: list[FileIn], context_snippets: list[str]) -> ReviewResult:
        import httpx

        resp = httpx.post(
            "https://api.anthropic.com/v1/messages",
            headers={
                "x-api-key": self._key,
                "anthropic-version": "2023-06-01",
                "content-type": "application/json",
            },
            json={
                "model": self.model,
                "max_tokens": 1500,
                "system": self._SYSTEM,
                "messages": [{"role": "user", "content": self._prompt(diff, context_snippets)}],
            },
            timeout=120,
        )
        resp.raise_for_status()
        blocks = resp.json().get("content", [])
        text = "".join(b.get("text", "") for b in blocks if b.get("type") == "text")
        return self._parse(text)


class OpenAIReviewer(_LLMReviewer):
    def __init__(self, settings: Settings) -> None:
        if not settings.openai_api_key:
            raise ValueError("OPENAI_API_KEY required for openai provider")
        self._key = settings.openai_api_key
        self.model = settings.llm_model

    def review(self, diff: str, files: list[FileIn], context_snippets: list[str]) -> ReviewResult:
        import httpx

        resp = httpx.post(
            "https://api.openai.com/v1/chat/completions",
            headers={"Authorization": f"Bearer {self._key}"},
            json={
                "model": self.model,
                "messages": [
                    {"role": "system", "content": self._SYSTEM},
                    {"role": "user", "content": self._prompt(diff, context_snippets)},
                ],
                "response_format": {"type": "json_object"},
            },
            timeout=120,
        )
        resp.raise_for_status()
        text = resp.json()["choices"][0]["message"]["content"]
        return self._parse(text)


def get_reviewer(settings: Settings) -> Reviewer:
    if settings.llm_provider == "anthropic":
        return AnthropicReviewer(settings)
    if settings.llm_provider == "openai":
        return OpenAIReviewer(settings)
    return MockReviewer()
