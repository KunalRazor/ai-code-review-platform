"""The analysis pipeline, built as a LangGraph state graph.

Nodes run in sequence:
    index_context -> retrieve -> review -> finalize

  index_context : embed the related-code context and load it into a vector store
  retrieve      : embed what changed and pull the nearest context (RAG)
  review        : hand the diff + retrieved context to the reviewer (LLM/mock)
  finalize      : filter by confidence and shape the response

Using a graph (rather than one function) makes the stages explicit and easy to
extend — e.g. adding a "triage cheap model then escalate" branch later.
"""

from __future__ import annotations

import hashlib
import logging
from typing import Any, TypedDict

from langgraph.graph import END, START, StateGraph

from .embeddings import get_embedder
from .llm import ReviewResult, get_reviewer
from .patch import added_lines, chunk_text
from .schemas import AnalyzeRequest, AnalyzeResponse, CommentOut
from .settings import Settings
from .vectorstore import Hit, InMemoryVectorStore, PgVectorStore

log = logging.getLogger("analyzer.graph")


class State(TypedDict, total=False):
    request: AnalyzeRequest
    store: Any
    change_text: str
    hits: list[Hit]
    result: ReviewResult
    response: AnalyzeResponse


class Pipeline:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self.embedder = get_embedder(settings)
        self.reviewer = get_reviewer(settings)
        self._pg = self._maybe_pg(settings)
        self.graph = self._build()

    def _maybe_pg(self, s: Settings) -> PgVectorStore | None:
        if not s.database_url:
            return None
        try:
            store = PgVectorStore(s.database_url, self.embedder.dim)
            log.info("using pgvector store for RAG")
            return store
        except Exception as exc:  # noqa: BLE001 - fall back gracefully
            log.warning("pgvector unavailable (%s); using in-memory store", exc)
            return None

    def _build(self):
        g = StateGraph(State)
        g.add_node("index_context", self._index_context)
        g.add_node("retrieve", self._retrieve)
        g.add_node("review", self._review)
        g.add_node("finalize", self._finalize)
        g.add_edge(START, "index_context")
        g.add_edge("index_context", "retrieve")
        g.add_edge("retrieve", "review")
        g.add_edge("review", "finalize")
        g.add_edge("finalize", END)
        return g.compile()

    def run(self, req: AnalyzeRequest) -> AnalyzeResponse:
        out = self.graph.invoke({"request": req})
        return out["response"]

    # ---- nodes ----

    def _index_context(self, state: State) -> State:
        req = state["request"]
        # Persistent pgvector corpus, or a fresh in-memory store scoped to this PR.
        store = self._pg if self._pg is not None else InMemoryVectorStore()

        for path, content in req.context.items():
            chunks = chunk_text(content)
            if not chunks:
                continue
            vecs = self.embedder.embed(chunks)
            for chunk, vec in zip(chunks, vecs):
                h = hashlib.md5(chunk.encode()).hexdigest()
                store.add(req.repo, path, h, chunk, vec)

        # What changed: the added lines across all files.
        changed: list[str] = []
        for f in req.files:
            changed.extend(al.text for al in added_lines(f.patch))
        change_text = "\n".join(changed) or req.diff

        return {"store": store, "change_text": change_text}

    def _retrieve(self, state: State) -> State:
        req = state["request"]
        store = state["store"]
        change_text = state.get("change_text", "")
        if not change_text.strip():
            return {"hits": []}
        qvec = self.embedder.embed([change_text])[0]
        hits = store.search(req.repo, qvec, self.settings.rag_top_k)
        return {"hits": hits}

    def _review(self, state: State) -> State:
        req = state["request"]
        snippets = [h.text for h in state.get("hits", [])]
        result = self.reviewer.review(req.diff, req.files, snippets)
        return {"result": result}

    def _finalize(self, state: State) -> State:
        req = state["request"]
        result = state["result"]
        threshold = max(req.min_confidence, 0.0)
        comments = [
            CommentOut(
                file=c.file, line=c.line, severity=c.severity,
                confidence=c.confidence, message=c.message, suggestion=c.suggestion,
            )
            for c in result.comments
            if c.confidence >= threshold and c.line > 0
        ]
        return {"response": AnalyzeResponse(summary=result.summary, model=result.model, comments=comments)}
