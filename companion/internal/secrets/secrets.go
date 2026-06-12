package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	ErrInvalidRef        = errors.New("invalid secret ref")
	ErrSecretNotFound    = errors.New("secret not found")
	ErrUnsupportedScheme = errors.New("unsupported secret ref scheme")

	envNameExpression = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Resolver interface {
	Resolve(ctx context.Context, ref string) (Secret, error)
}

type Secret struct {
	ref     string
	value   string
	version string
}

func NewSecret(ref, value, version string) (Secret, error) {
	if err := ValidateRef(ref); err != nil {
		return Secret{}, err
	}
	if value == "" {
		return Secret{}, ErrSecretNotFound
	}
	return Secret{ref: strings.TrimSpace(ref), value: value, version: strings.TrimSpace(version)}, nil
}

func (s Secret) Ref() string {
	return s.ref
}

func (s Secret) Value() string {
	return s.value
}

func (s Secret) Version() string {
	return s.version
}

func (s Secret) String() string {
	if s.ref == "" {
		return "[secret:redacted]"
	}
	return fmt.Sprintf("[secret:%s:redacted]", s.ref)
}

func (s Secret) GoString() string {
	return s.String()
}

func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"ref":      s.ref,
		"version":  s.version,
		"redacted": true,
	})
}

type EnvResolver struct {
	lookup func(string) (string, bool)
}

func NewEnvResolver() EnvResolver {
	return EnvResolver{lookup: os.LookupEnv}
}

func NewEnvResolverWithLookup(lookup func(string) (string, bool)) EnvResolver {
	return EnvResolver{lookup: lookup}
}

func (r EnvResolver) Resolve(_ context.Context, ref string) (Secret, error) {
	parsed, err := ParseRef(ref)
	if err != nil {
		return Secret{}, err
	}
	if parsed.Scheme != "env" {
		return Secret{}, fmt.Errorf("%w: %s", ErrUnsupportedScheme, parsed.Scheme)
	}
	lookup := r.lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}
	value, ok := lookup(parsed.Name)
	if !ok || value == "" {
		return Secret{}, fmt.Errorf("%w: %s", ErrSecretNotFound, parsed.Name)
	}
	return Secret{ref: parsed.Raw, value: value}, nil
}

type ChainResolver []Resolver

func (r ChainResolver) Resolve(ctx context.Context, ref string) (Secret, error) {
	var lastErr error
	for _, resolver := range r {
		if resolver == nil {
			continue
		}
		secret, err := resolver.Resolve(ctx, ref)
		if err == nil {
			return secret, nil
		}
		lastErr = err
		if !errors.Is(err, ErrUnsupportedScheme) {
			return Secret{}, err
		}
	}
	if lastErr != nil {
		return Secret{}, lastErr
	}
	return Secret{}, ErrUnsupportedScheme
}

type Ref struct {
	Raw    string
	Scheme string
	Name   string
}

func ParseRef(ref string) (Ref, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Ref{}, fmt.Errorf("%w: ref is required", ErrInvalidRef)
	}
	if strings.ContainsAny(ref, " \t\r\n") {
		return Ref{}, fmt.Errorf("%w: ref must not contain whitespace", ErrInvalidRef)
	}
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return Ref{}, fmt.Errorf("%w: ref must use scheme:name syntax", ErrInvalidRef)
	}
	scheme := strings.TrimSpace(strings.ToLower(parts[0]))
	name := strings.TrimSpace(parts[1])
	if scheme == "" || name == "" {
		return Ref{}, fmt.Errorf("%w: scheme and name are required", ErrInvalidRef)
	}
	switch scheme {
	case "env":
		if !envNameExpression.MatchString(name) {
			return Ref{}, fmt.Errorf("%w: env ref must use an environment variable name", ErrInvalidRef)
		}
	case "vault", "secretmanager":
		// Valid references for production adapters. The local/dev EnvResolver
		// intentionally does not resolve them.
	default:
		return Ref{}, fmt.Errorf("%w: %s", ErrUnsupportedScheme, scheme)
	}
	return Ref{Raw: ref, Scheme: scheme, Name: name}, nil
}

func ValidateRef(ref string) error {
	_, err := ParseRef(ref)
	return err
}
