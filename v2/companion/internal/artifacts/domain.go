package artifacts

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	MaxArtifactBytes   int64 = 250 << 20
	MaxDiagnosisBytes  int64 = 500 << 20
	MaxRepositoryBytes int64 = 5 << 30
	StagingTTL               = 24 * time.Hour
)

var (
	ErrArtifactTooLarge      = errors.New("artifact exceeds size limit")
	ErrDiagnosisTooLarge     = errors.New("diagnosis exceeds size limit")
	ErrChecksumMismatch      = errors.New("artifact checksum mismatch")
	ErrSizeMismatch          = errors.New("artifact size mismatch")
	ErrMIMEMismatch          = errors.New("artifact MIME does not match content")
	ErrUnsupportedFormat     = errors.New("artifact format is not supported")
	ErrEmptyDerivative       = errors.New("artifact adapter returned no usable content")
	ErrIndexingFailed        = errors.New("artifact indexing failed")
	ErrExtractionUnavailable = errors.New("isolated artifact extraction is unavailable")
	ErrArtifactStoreFull     = errors.New("artifact staging store is full")
)

type Scope struct {
	OrgID                string    `json:"org_id"`
	VirployeeID          uuid.UUID `json:"virployee_id"`
	ProductSurface       string    `json:"product_surface"`
	SubjectID            string    `json:"subject_id"`
	RepositoryGeneration string    `json:"repository_generation"`
}

type Manifest struct {
	DocumentID string `json:"document_id"`
	Name       string `json:"name"`
	SourceRef  string `json:"source_ref,omitempty"`
	ReadURL    string `json:"read_url,omitempty"`
	SHA256     string `json:"sha256"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
	Required   bool   `json:"required,omitempty"`
}

type Status string

const (
	StatusReceived   Status = "received"
	StatusStaging    Status = "staging"
	StatusStaged     Status = "staged"
	StatusExtracting Status = "extracting"
	StatusExtracted  Status = "extracted"
	StatusIndexing   Status = "indexing"
	StatusIndexed    Status = "indexed"
	StatusFailed     Status = "failed"
)

type Record struct {
	ID         uuid.UUID
	Scope      Scope
	Manifest   Manifest
	Status     Status
	StagedURI  string
	ActualMIME string
	ErrorCode  string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type StoredArtifact struct {
	URI       string
	MIMEType  string
	SHA256    string
	SizeBytes int64
	ExpiresAt time.Time
}

type PartKind string

const (
	PartText       PartKind = "text"
	PartInlineData PartKind = "inline_data"
	PartFileData   PartKind = "file_data"
)

type Locator struct {
	Page    int       `json:"page,omitempty"`
	Slide   int       `json:"slide,omitempty"`
	Sheet   string    `json:"sheet,omitempty"`
	BBox    []float64 `json:"bbox,omitempty"`
	StartMS int64     `json:"start_ms,omitempty"`
	EndMS   int64     `json:"end_ms,omitempty"`
	Frame   int       `json:"frame,omitempty"`
}

type ContentPart struct {
	Kind       PartKind `json:"kind"`
	Text       string   `json:"text,omitempty"`
	Data       []byte   `json:"data,omitempty"`
	URI        string   `json:"uri,omitempty"`
	MIMEType   string   `json:"mime_type,omitempty"`
	Name       string   `json:"name,omitempty"`
	SHA256     string   `json:"sha256,omitempty"`
	DocumentID string   `json:"document_id,omitempty"`
	Locator    *Locator `json:"locator,omitempty"`
}

// Blob is a replayable, bounded local spool. Every consumer gets a fresh reader
// so scanning, staging and extraction cannot accidentally consume each other.
type Blob interface {
	Open() (io.ReadCloser, error)
	Size() int64
}

type AdaptInput struct {
	Scope    Scope
	Manifest Manifest
	Stored   StoredArtifact
	Blob     Blob
}

type IngestRequest struct {
	Scope           Scope
	Artifacts       []Manifest
	RequireIndexing bool
	Progress        func(context.Context, Status) error
}

// UploadRequest carries a caller-owned stream into the same verified
// scan/stage/extract/index pipeline used by remote manifests. The stream is
// spooled before the catalog write so its immutable size and checksum, rather
// than client-provided metadata, become the artifact provenance.
type UploadRequest struct {
	Scope       Scope
	Manifest    Manifest
	Content     io.Reader
	ContentType string
	// RequireIndexing makes missing index infrastructure a pre-processing
	// failure. Knowledge documents use this because extraction alone is not a
	// registrable source.
	RequireIndexing bool
	Progress        func(context.Context, Status) error
}

type IngestResult struct {
	Records []Record
	Parts   []ContentPart
}

// Function ports are intentionally context-aware; implementations can be local
// libraries, isolated converters, Runtime endpoints, or managed services.
type ExtractRequest struct {
	Scope    Scope
	Manifest Manifest
	Stored   StoredArtifact
	Blob     Blob
	Profile  string
}

type Chunk struct {
	ID               string
	Text             string
	MIMEType         string
	SHA256           string
	DocumentID       string
	Locator          *Locator
	SourceVersion    string
	ExtractorVersion string
	ChunkerVersion   string
}

type Embedding struct {
	ChunkID string
	Values  []float32
	Model   string
}

type RetrievalQuery struct {
	Scope Scope
	Text  string
	Limit int
}

type RetrievalHit struct {
	Chunk Chunk
	Score float64
}

type AnswerRequest struct {
	Scope Scope
	Parts []ContentPart
}

type Answer struct {
	Output []byte
}
