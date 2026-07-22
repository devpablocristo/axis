CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS companion_artifact_chunks (
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL,
    product_surface text NOT NULL,
    subject_id text NOT NULL,
    repository_generation text NOT NULL,
    document_id text NOT NULL,
    chunk_id text NOT NULL,
    content text NOT NULL CHECK (btrim(content) <> ''),
    mime_type text NOT NULL DEFAULT 'text/plain',
    source_sha256 text NOT NULL,
    locator jsonb NOT NULL DEFAULT '{}'::jsonb,
    source_version text NOT NULL,
    extractor_version text NOT NULL,
    chunker_version text NOT NULL,
    embedding_model text NOT NULL,
    embedding_dimensions smallint NOT NULL CHECK (embedding_dimensions = 768),
    embedding vector(768) NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (
        tenant_id, virployee_id, product_surface, subject_id,
        repository_generation, embedding_model, chunk_id
    )
);

CREATE INDEX IF NOT EXISTS companion_artifact_chunks_scope_idx
    ON companion_artifact_chunks (
        tenant_id, virployee_id, product_surface, subject_id,
        repository_generation, embedding_model, document_id
    );

CREATE INDEX IF NOT EXISTS companion_artifact_chunks_fts_idx
    ON companion_artifact_chunks USING gin (search_vector);

CREATE INDEX IF NOT EXISTS companion_artifact_chunks_embedding_idx
    ON companion_artifact_chunks USING hnsw (embedding vector_cosine_ops);
