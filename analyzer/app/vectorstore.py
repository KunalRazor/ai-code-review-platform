"""Vector stores for RAG retrieval.

Two backends with the same interface:
  - InMemoryVectorStore: pure-Python cosine search, used offline (no DB).
  - PgVectorStore: persists embeddings in Postgres via pgvector and searches
    with the <=> cosine-distance operator; dedupes by (path, content_hash) so
    unchanged files are never re-embedded.
"""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np


@dataclass
class Hit:
    path: str
    text: str
    score: float


class InMemoryVectorStore:
    def __init__(self) -> None:
        self._items: list[tuple[str, str, str, np.ndarray]] = []  # repo, path, text, vec

    def add(self, repo: str, path: str, content_hash: str, text: str, vec: list[float]) -> None:
        self._items.append((repo, path, text, np.asarray(vec, dtype=np.float32)))

    def search(self, repo: str, vec: list[float], k: int) -> list[Hit]:
        q = np.asarray(vec, dtype=np.float32)
        qn = float(np.linalg.norm(q)) or 1.0
        hits: list[Hit] = []
        for r, path, text, v in self._items:
            if r != repo:
                continue
            denom = (float(np.linalg.norm(v)) * qn) or 1.0
            hits.append(Hit(path=path, text=text, score=float(np.dot(q, v)) / denom))
        hits.sort(key=lambda h: h.score, reverse=True)
        return hits[:k]


class PgVectorStore:
    """pgvector-backed store. Assumes a reachable Postgres; the caller falls
    back to in-memory if construction raises."""

    def __init__(self, dsn: str, dim: int) -> None:
        import psycopg
        from pgvector.psycopg import register_vector

        self._conn = psycopg.connect(dsn, autocommit=True)
        self._conn.execute("CREATE EXTENSION IF NOT EXISTS vector")
        register_vector(self._conn)
        self._conn.execute(
            f"""
            CREATE TABLE IF NOT EXISTS file_embeddings (
                id bigserial PRIMARY KEY,
                repo text NOT NULL,
                file_path text NOT NULL,
                content_hash text NOT NULL,
                chunk text NOT NULL,
                embedding vector({dim}) NOT NULL
            )
            """
        )
        self._conn.execute(
            "CREATE INDEX IF NOT EXISTS idx_file_embeddings_repo ON file_embeddings (repo)"
        )

    def add(self, repo: str, path: str, content_hash: str, text: str, vec: list[float]) -> None:
        exists = self._conn.execute(
            "SELECT 1 FROM file_embeddings WHERE repo=%s AND file_path=%s AND content_hash=%s LIMIT 1",
            (repo, path, content_hash),
        ).fetchone()
        if exists:
            return
        self._conn.execute(
            "INSERT INTO file_embeddings (repo, file_path, content_hash, chunk, embedding) "
            "VALUES (%s, %s, %s, %s, %s)",
            (repo, path, content_hash, text, np.asarray(vec, dtype=np.float32)),
        )

    def search(self, repo: str, vec: list[float], k: int) -> list[Hit]:
        q = np.asarray(vec, dtype=np.float32)
        rows = self._conn.execute(
            "SELECT file_path, chunk, 1 - (embedding <=> %s) AS score "
            "FROM file_embeddings WHERE repo=%s ORDER BY embedding <=> %s LIMIT %s",
            (q, repo, q, k),
        ).fetchall()
        return [Hit(path=r[0], text=r[1], score=float(r[2])) for r in rows]
