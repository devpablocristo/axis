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
	OrgID                string `json:"org_id"`
	VirployeeID          string `json:"virployee_id,omitempty"`
	ProductSurface       string `json:"product_surface,omitempty"`
	SubjectID            string `json:"subject_id,omitempty"`
	RepositoryGeneration string `json:"repository_generation,omitempty"`
}

type Manifest struct {
	DocumentID string `json:"document_id"`
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	MIMEType   string `json:"mime_type"`
	SizeBytes  int64  `json:"size_bytes"`
}

type Metadata struct {
	Scope    Scope    `json:"scope"`
	Manifest Manifest `json:"manifest"`
	Profile  string   `json:"profile"`
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
