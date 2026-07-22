package artifactindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/devpablocristo/companion-v2/internal/artifacts"
)

const (
	DefaultChunkRunes   = 1200
	DefaultOverlapRunes = 200
	DefaultChunkVersion = "clinical-runes-v1"
)

type Chunker struct {
	MaxRunes, OverlapRunes    int
	Version, ExtractorVersion string
}

func NewChunker() *Chunker {
	return &Chunker{MaxRunes: DefaultChunkRunes, OverlapRunes: DefaultOverlapRunes, Version: DefaultChunkVersion, ExtractorVersion: "artifact-adapters-v1"}
}

func (c *Chunker) Chunk(_ context.Context, _ artifacts.Scope, parts []artifacts.ContentPart) ([]artifacts.Chunk, error) {
	if c.MaxRunes <= 0 || c.OverlapRunes < 0 || c.OverlapRunes >= c.MaxRunes {
		return nil, errors.New("invalid chunker bounds")
	}
	out := make([]artifacts.Chunk, 0)
	for _, part := range parts {
		if part.Kind != artifacts.PartText || strings.TrimSpace(part.Text) == "" {
			continue
		}
		runes := []rune(part.Text)
		for start := 0; start < len(runes); {
			end := start + c.MaxRunes
			if end >= len(runes) {
				end = len(runes)
			} else {
				minimum := start + c.MaxRunes*3/5
				for candidate := end; candidate > minimum; candidate-- {
					if unicode.IsSpace(runes[candidate-1]) {
						end = candidate
						break
					}
				}
			}
			text := strings.TrimSpace(string(runes[start:end]))
			if text != "" {
				locatorJSON, _ := json.Marshal(part.Locator)
				hash := sha256.Sum256([]byte(strings.Join([]string{
					part.DocumentID, part.SHA256, string(locatorJSON), c.Version,
					text,
				}, "\x00")))
				out = append(out, artifacts.Chunk{
					ID: hex.EncodeToString(hash[:]), Text: text, MIMEType: part.MIMEType,
					SHA256: part.SHA256, DocumentID: part.DocumentID, Locator: part.Locator,
					SourceVersion: part.SHA256, ExtractorVersion: c.ExtractorVersion, ChunkerVersion: c.Version,
				})
			}
			if end == len(runes) {
				break
			}
			start = end - c.OverlapRunes
		}
	}
	return out, nil
}
