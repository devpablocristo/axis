package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const localArtifactScheme = "axis-local-artifact"

type LocalStoreConfig struct {
	RootDir  string
	MaxBytes int64
}

// LocalStore is a development/test staging adapter. It uses opaque paths,
// private permissions, atomic writes and a process-local capacity reservation.
// Production wiring never constructs it.
type LocalStore struct {
	root     string
	maxBytes int64
	mu       sync.Mutex
	used     int64
	now      func() time.Time
}

func NewLocalStore(config LocalStoreConfig) (*LocalStore, error) {
	root := filepath.Clean(strings.TrimSpace(config.RootDir))
	if root == "." || !filepath.IsAbs(root) {
		return nil, errors.New("local artifact store requires an absolute root directory")
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = MaxRepositoryBytes
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create local artifact root: %w", err)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect local artifact root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("local artifact root must be a real directory")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("secure local artifact root: %w", err)
	}
	used, err := localStoreUsage(root)
	if err != nil {
		return nil, err
	}
	if used > config.MaxBytes {
		return nil, ErrArtifactStoreFull
	}
	return &LocalStore{root: root, maxBytes: config.MaxBytes, used: used, now: time.Now}, nil
}

func (s *LocalStore) PutOriginal(_ context.Context, scope Scope, manifest Manifest, blob Blob) (StoredArtifact, error) {
	if blob == nil || blob.Size() < 0 || blob.Size() > MaxArtifactBytes {
		return StoredArtifact{}, ErrArtifactTooLarge
	}
	checksum := strings.ToLower(strings.TrimSpace(manifest.SHA256))
	if len(checksum) != sha256.Size*2 {
		return StoredArtifact{}, errors.New("local artifact requires a verified SHA-256")
	}
	if _, err := hex.DecodeString(checksum); err != nil {
		return StoredArtifact{}, errors.New("local artifact requires a verified SHA-256")
	}
	relative := localObjectPath(scope, manifest)
	target, err := s.resolveRelative(relative)
	if err != nil {
		return StoredArtifact{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if info, statErr := os.Lstat(target); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return StoredArtifact{}, errors.New("local artifact target is not a regular file")
		}
		if err := verifyLocalArtifact(target, checksum, manifest.SizeBytes); err != nil {
			return StoredArtifact{}, err
		}
		return s.stored(relative, manifest, info.Size()), nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return StoredArtifact{}, statErr
	}
	if blob.Size() > s.maxBytes-s.used {
		return StoredArtifact{}, ErrArtifactStoreFull
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return StoredArtifact{}, fmt.Errorf("create local artifact directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".axis-upload-*")
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("create local artifact temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return StoredArtifact{}, err
	}
	reader, err := blob.Open()
	if err != nil {
		return StoredArtifact{}, err
	}
	defer func() { _ = reader.Close() }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(reader, MaxArtifactBytes+1))
	if err != nil {
		return StoredArtifact{}, fmt.Errorf("write local artifact: %w", err)
	}
	if written > MaxArtifactBytes {
		return StoredArtifact{}, ErrArtifactTooLarge
	}
	if written != blob.Size() || (manifest.SizeBytes > 0 && written != manifest.SizeBytes) {
		return StoredArtifact{}, ErrSizeMismatch
	}
	if hex.EncodeToString(hash.Sum(nil)) != checksum {
		return StoredArtifact{}, ErrChecksumMismatch
	}
	if err := temporary.Sync(); err != nil {
		return StoredArtifact{}, fmt.Errorf("sync local artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return StoredArtifact{}, fmt.Errorf("close local artifact: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return StoredArtifact{}, fmt.Errorf("commit local artifact: %w", err)
	}
	committed = true
	s.used += written
	return s.stored(relative, manifest, written), nil
}

func (s *LocalStore) GetOriginal(_ context.Context, stored StoredArtifact, dst io.Writer) (string, int64, error) {
	ref, err := url.Parse(strings.TrimSpace(stored.URI))
	if err != nil || ref.Scheme != localArtifactScheme || ref.Host != "store" || ref.User != nil || ref.RawQuery != "" || ref.Fragment != "" {
		return "", 0, errors.New("staged artifact URI is outside the local store")
	}
	fullPath, err := s.resolveRelative(strings.TrimPrefix(ref.Path, "/"))
	if err != nil {
		return "", 0, err
	}
	info, err := os.Lstat(fullPath)
	if err != nil {
		return "", 0, fmt.Errorf("inspect local artifact: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > MaxArtifactBytes {
		return "", 0, errors.New("local artifact is not a bounded regular file")
	}
	reader, err := os.Open(fullPath)
	if err != nil {
		return "", 0, fmt.Errorf("open local artifact: %w", err)
	}
	defer func() { _ = reader.Close() }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(reader, MaxArtifactBytes+1))
	if err != nil {
		return "", written, fmt.Errorf("read local artifact: %w", err)
	}
	if written > MaxArtifactBytes {
		return "", written, ErrArtifactTooLarge
	}
	if stored.SizeBytes > 0 && written != stored.SizeBytes {
		return "", written, ErrSizeMismatch
	}
	if expected := strings.ToLower(strings.TrimSpace(stored.SHA256)); expected != "" && hex.EncodeToString(hash.Sum(nil)) != expected {
		return "", written, ErrChecksumMismatch
	}
	return stored.MIMEType, written, nil
}

func (s *LocalStore) stored(relative string, manifest Manifest, size int64) StoredArtifact {
	uri := (&url.URL{Scheme: localArtifactScheme, Host: "store", Path: "/" + filepath.ToSlash(relative)}).String()
	return StoredArtifact{
		URI: uri, MIMEType: manifest.MIMEType, SHA256: manifest.SHA256,
		SizeBytes: size, ExpiresAt: s.now().UTC().Add(StagingTTL),
	}
}

func (s *LocalStore) resolveRelative(relative string) (string, error) {
	relative = filepath.Clean(filepath.FromSlash(strings.TrimSpace(relative)))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("staged artifact path escapes the local store")
	}
	fullPath := filepath.Join(s.root, relative)
	within, err := filepath.Rel(s.root, fullPath)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", errors.New("staged artifact path escapes the local store")
	}
	return fullPath, nil
}

func localObjectPath(scope Scope, manifest Manifest) string {
	identity := strings.Join([]string{
		scope.OrgID, scope.VirployeeID.String(), scope.ProductSurface, scope.SubjectID,
		scope.RepositoryGeneration, manifest.DocumentID, strings.ToLower(strings.TrimSpace(manifest.SHA256)),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	key := hex.EncodeToString(sum[:])
	return filepath.Join("objects", key[:2], key, "original")
}

func verifyLocalArtifact(filename, checksum string, expectedSize int64) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, MaxArtifactBytes+1))
	if err != nil {
		return err
	}
	if written > MaxArtifactBytes {
		return ErrArtifactTooLarge
	}
	if expectedSize > 0 && written != expectedSize {
		return ErrSizeMismatch
	}
	if hex.EncodeToString(hash.Sum(nil)) != checksum {
		return ErrChecksumMismatch
	}
	return nil
}

func localStoreUsage(root string) (int64, error) {
	var used int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("local artifact store contains symlink %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("local artifact store contains non-regular file %s", path)
		}
		if info.Size() > MaxArtifactBytes || used > MaxRepositoryBytes-info.Size() {
			return ErrArtifactStoreFull
		}
		used += info.Size()
		return nil
	})
	return used, err
}
