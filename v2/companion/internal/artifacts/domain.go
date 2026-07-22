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
)

type Scope struct {
	TenantID             string
	VirployeeID          uuid.UUID
	ProductSurface       string
	SubjectID            string
	RepositoryGeneration string
}

type Manifest struct {
	DocumentID string
	Name       string
	SourceRef  string
	ReadURL    string
	SHA256     string
	MIMEType   string
	SizeBytes  int64
	Required   bool
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
	Scope     Scope
	Artifacts []Manifest
	Progress  func(context.Context, Status) error
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
