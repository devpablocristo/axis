package extractor

import "errors"

const (
	MaxArtifactBytes   int64 = 250 << 20
	MaxDerivativeBytes int64 = 64 << 20
)

var (
	ErrInvalidRequest = errors.New("invalid extraction request")
	ErrUnsupported    = errors.New("unsupported extraction profile")
	ErrUnavailable    = errors.New("extractor dependency is unavailable")
	ErrOutputTooLarge = errors.New("extraction output exceeds limit")
)

type Scope struct {
	TenantID             string
	VirployeeID          string
	ProductSurface       string
	SubjectID            string
	RepositoryGeneration string
}

type Manifest struct {
	DocumentID string
	Name       string
	SHA256     string
	MIMEType   string
	SizeBytes  int64
}

type Metadata struct {
	Scope    Scope
	Manifest Manifest
	Profile  string
}

type Locator struct {
	Page    int       `json:"page,omitempty"`
	Slide   int       `json:"slide,omitempty"`
	Sheet   string    `json:"sheet,omitempty"`
	BBox    []float64 `json:"bbox,omitempty"`
	StartMS int64     `json:"start_ms,omitempty"`
	EndMS   int64     `json:"end_ms,omitempty"`
	Frame   int       `json:"frame,omitempty"`
}

type Part struct {
	Kind       string   `json:"kind"`
	Text       string   `json:"text,omitempty"`
	Data       []byte   `json:"data,omitempty"`
	MIMEType   string   `json:"mime_type,omitempty"`
	Name       string   `json:"name,omitempty"`
	DocumentID string   `json:"document_id,omitempty"`
	SHA256     string   `json:"sha256,omitempty"`
	Locator    *Locator `json:"locator,omitempty"`
}

type Response struct {
	Parts []Part `json:"parts"`
}
