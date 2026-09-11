"""AI Code Review analyzer service.

FastAPI + LangGraph pipeline that turns a PR diff into a structured review:
chunk -> embed -> retrieve related code (RAG) -> LLM -> validated JSON.
Embedding and LLM providers are pluggable and default to offline-capable mocks.
"""
