CREATE TABLE IF NOT EXISTS companion_artifacts (
    id uuid PRIMARY KEY,
    tenant_id text NOT NULL,
    virployee_id uuid NOT NULL,
    product_surface text NOT NULL,
    subject_id text NOT NULL,
    repository_generation text NOT NULL,
    document_id text NOT NULL,
    name text NOT NULL DEFAULT '',
    source_ref text NOT NULL DEFAULT '',
    sha256 text NOT NULL,
    mime_type text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0 AND size_bytes <= 262144000),
    required boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'received'
        CHECK (status IN ('received','staging','staged','extracting','extracted','indexing','indexed','failed')),
    staged_uri text NOT NULL DEFAULT '',
    actual_mime text NOT NULL DEFAULT '',
    error_code text NOT NULL DEFAULT '',
    expires_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, virployee_id, product_surface, subject_id, repository_generation, document_id)
);

CREATE INDEX IF NOT EXISTS companion_artifacts_generation_idx
    ON companion_artifacts (tenant_id, virployee_id, product_surface, subject_id, repository_generation, document_id);

CREATE INDEX IF NOT EXISTS companion_artifacts_staging_expiry_idx
    ON companion_artifacts (expires_at, id)
    WHERE expires_at IS NOT NULL AND status IN ('staged','extracted','indexed');

-- Product signed URLs are deliberately absent. The durable assist row owns the
-- transport hint until staging; the catalog stores only stable manifest data.
