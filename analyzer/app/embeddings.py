"""Embedding providers.

Default is HashEmbedder: a deterministic, dependency-free bag-of-words hash
that needs no model download or API key, so the service embeds and retrieves
offline. Set EMBEDDINGS_PROVIDER=local (sentence-transformers) or =openai for
real semantic embeddings.
"""

from __future__ import annotations

import hashlib
import re
from typing import Protocol

import numpy as np

from .settings import Settings

_TOKEN_RE = re.compile(r"[A-Za-z_][A-Za-z0-9_]+")


class Embedder(Protocol):
    @property
    def dim(self) -> int: ...

    def embed(self, texts: list[str]) -> list[list[float]]: ...


class HashEmbedder:
    """Hash each token into a fixed-dimension vector (bag-of-words), then
    L2-normalize. Similar code shares tokens and lands near each other, which
    is enough to retrieve related snippets for context."""

    def __init__(self, dim: int = 256) -> None:
        self._dim = dim

    @property
    def dim(self) -> int:
        return self._dim

    def embed(self, texts: list[str]) -> list[list[float]]:
        out: list[list[float]] = []
        for text in texts:
            vec = np.zeros(self._dim, dtype=np.float32)
            for tok in _TOKEN_RE.findall(text.lower()):
                idx = int(hashlib.md5(tok.encode()).hexdigest(), 16) % self._dim
                vec[idx] += 1.0
            norm = float(np.linalg.norm(vec))
            if norm > 0:
                vec /= norm
            out.append(vec.tolist())
        return out


class OpenAIEmbedder:
    """Calls the OpenAI embeddings API. Used only when EMBEDDINGS_PROVIDER=openai."""

    def __init__(self, settings: Settings) -> None:
        if not settings.openai_api_key:
            raise ValueError("OPENAI_API_KEY required for openai embeddings")
        self._key = settings.openai_api_key
        self._model = "text-embedding-3-small"
        self._dim = 1536

    @property
    def dim(self) -> int:
        return self._dim

    def embed(self, texts: list[str]) -> list[list[float]]:
        import httpx

        resp = httpx.post(
            "https://api.openai.com/v1/embeddings",
            headers={"Authorization": f"Bearer {self._key}"},
            json={"model": self._model, "input": texts},
            timeout=60,
        )
        resp.raise_for_status()
        data = resp.json()["data"]
        return [row["embedding"] for row in data]


def get_embedder(settings: Settings) -> Embedder:
    provider = settings.embeddings_provider
    if provider == "openai":
        return OpenAIEmbedder(settings)
    if provider == "local":
        # Optional heavy dependency; imported lazily so it is not required.
        from sentence_transformers import SentenceTransformer  # type: ignore

        class _Local:
            def __init__(self) -> None:
                self._m = SentenceTransformer("all-MiniLM-L6-v2")

            @property
            def dim(self) -> int:
                return 384

            def embed(self, texts: list[str]) -> list[list[float]]:
                return [v.tolist() for v in self._m.encode(texts, normalize_embeddings=True)]

        return _Local()
    return HashEmbedder(settings.embedding_dim)
