"""FastAPI entrypoint for the analyzer service."""

from __future__ import annotations

import logging

from fastapi import FastAPI

from .graph import Pipeline
from .schemas import AnalyzeRequest, AnalyzeResponse
from .settings import load

logging.basicConfig(level=logging.INFO)

settings = load()
pipeline = Pipeline(settings)

app = FastAPI(title="AI Code Review Analyzer", version="1.0.0")


@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {
        "status": "ok",
        "embeddings": settings.embeddings_provider,
        "llm": settings.llm_provider,
    }


@app.post("/analyze", response_model=AnalyzeResponse)
def analyze(req: AnalyzeRequest) -> AnalyzeResponse:
    return pipeline.run(req)
