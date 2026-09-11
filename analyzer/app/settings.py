"""Runtime settings, loaded from environment variables.

Defaults are chosen so the service runs fully offline: a deterministic hash
embedder and a heuristic mock reviewer, with no database. Set the provider
envs to switch to real embeddings / LLMs, and DATABASE_URL to use pgvector.
"""

from __future__ import annotations

import os
from dataclasses import dataclass


@dataclass(frozen=True)
class Settings:
    embeddings_provider: str  # "hash" (default) | "local" | "openai"
    llm_provider: str  # "mock" (default) | "anthropic" | "openai"
    embedding_dim: int
    database_url: str | None
    anthropic_api_key: str | None
    openai_api_key: str | None
    llm_model: str
    rag_top_k: int


def load() -> Settings:
    return Settings(
        embeddings_provider=os.getenv("EMBEDDINGS_PROVIDER", "hash"),
        llm_provider=os.getenv("LLM_PROVIDER", "mock"),
        embedding_dim=int(os.getenv("EMBEDDING_DIM", "256")),
        database_url=os.getenv("DATABASE_URL") or None,
        anthropic_api_key=os.getenv("ANTHROPIC_API_KEY") or None,
        openai_api_key=os.getenv("OPENAI_API_KEY") or None,
        llm_model=os.getenv("LLM_MODEL", "claude-opus-4-6"),
        rag_top_k=int(os.getenv("RAG_TOP_K", "3")),
    )
