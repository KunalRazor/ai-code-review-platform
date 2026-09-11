"""Pydantic request/response models. Field names match the JSON the Go worker
sends (snake_case), so no aliases are needed."""

from __future__ import annotations

from pydantic import BaseModel, Field


class FileIn(BaseModel):
    filename: str
    status: str = ""
    patch: str = ""
    additions: int = 0
    deletions: int = 0


class AnalyzeRequest(BaseModel):
    repo: str = ""
    pr_number: int = 0
    diff: str = ""
    files: list[FileIn] = Field(default_factory=list)
    # path -> current file content, used as RAG context (related code).
    context: dict[str, str] = Field(default_factory=dict)
    min_confidence: float = 0.0


class CommentOut(BaseModel):
    file: str
    line: int
    severity: str  # info | low | medium | high
    confidence: float
    message: str
    suggestion: str = ""


class AnalyzeResponse(BaseModel):
    summary: str
    model: str
    comments: list[CommentOut] = Field(default_factory=list)
