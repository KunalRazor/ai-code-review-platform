-- pgvector extension for the analyzer's file_embeddings table.
-- The Go store creates its own tables via GORM AutoMigrate; the analyzer
-- creates file_embeddings itself with the configured embedding dimension.
CREATE EXTENSION IF NOT EXISTS vector;
