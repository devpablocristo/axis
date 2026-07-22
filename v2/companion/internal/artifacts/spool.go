package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

type fileBlob struct {
	path string
	size int64
}

func (b *fileBlob) Open() (io.ReadCloser, error) { return os.Open(b.path) }
func (b *fileBlob) Size() int64                  { return b.size }
func (b *fileBlob) Close() error                 { return os.Remove(b.path) }

func spool(fetch func(io.Writer) (string, int64, error)) (*fileBlob, string, string, error) {
	f, err := os.CreateTemp("", "axis-artifact-*")
	if err != nil {
		return nil, "", "", fmt.Errorf("create artifact spool: %w", err)
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()
	limited := &limitWriter{writer: io.MultiWriter(f, hash), remaining: MaxArtifactBytes + 1}
	contentType, reportedSize, err := fetch(limited)
	if err != nil {
		return nil, "", "", err
	}
	if limited.written > MaxArtifactBytes {
		return nil, "", "", ErrArtifactTooLarge
	}
	if reportedSize >= 0 && reportedSize != limited.written {
		return nil, "", "", ErrSizeMismatch
	}
	if err := f.Sync(); err != nil {
		return nil, "", "", fmt.Errorf("sync artifact spool: %w", err)
	}
	ok = true
	return &fileBlob{path: path, size: limited.written}, contentType, hex.EncodeToString(hash.Sum(nil)), nil
}

type limitWriter struct {
	writer    io.Writer
	remaining int64
	written   int64
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		p = p[:w.remaining]
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	w.written += int64(n)
	if err == nil && w.remaining == 0 {
		return n, ErrArtifactTooLarge
	}
	return n, err
}
