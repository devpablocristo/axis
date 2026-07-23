package connectors

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	headerTimestamp         = "X-Axis-Timestamp"
	headerIdempotency       = "X-Axis-Idempotency-Key"
	headerSignature         = "X-Axis-Signature"
	headerResponseSignature = "X-Axis-Response-Signature"
)

type HTTPAdapter struct {
	descriptor Descriptor
	client     *http.Client
	secrets    SecretResolver
	validator  SchemaValidator
	now        func() time.Time
}

func NewHTTPAdapter(descriptor Descriptor, environment string, client *http.Client, secrets SecretResolver, validator SchemaValidator) (*HTTPAdapter, error) {
	normalized, err := NormalizeDescriptor(descriptor, environment)
	if err != nil {
		return nil, err
	}
	if client == nil || secrets == nil || validator == nil {
		return nil, fmt.Errorf("connector HTTP dependencies are required")
	}
	return &HTTPAdapter{
		descriptor: normalized, client: client, secrets: secrets, validator: validator,
		now: func() time.Time { return time.Now().UTC() },
	}, nil
}

func (a *HTTPAdapter) Descriptor() Descriptor { return a.descriptor }

func (a *HTTPAdapter) Invoke(ctx context.Context, request InvokeRequest) (InvocationResult, error) {
	operation, err := a.validateInvokeRequest(request)
	if err != nil {
		return InvocationResult{}, err
	}
	body, err := marshalRequest(request)
	if err != nil {
		return InvocationResult{}, err
	}
	if int64(len(body)) > operation.MaxRequestBytes {
		return InvocationResult{}, fmt.Errorf("connector request exceeds operation limit")
	}
	endpoint := a.descriptor.BaseURL + "/v1/invocations"
	result, err := a.do(ctx, http.MethodPost, endpoint, request.IdempotencyKey, body, operation.MaxResponseBytes)
	if err != nil {
		var transportErr *TransportError
		if errors.As(err, &transportErr) && transportErr.Ambiguous {
			transportErr.InvocationID = request.InvocationID
		}
		return InvocationResult{}, err
	}
	if err := validResult(
		result,
		request.InvocationID,
		a.descriptor.ProductID,
		operation.CapabilityID,
		operation.Name,
	); err != nil {
		return InvocationResult{}, err
	}
	if result.Status == "succeeded" {
		if err := a.validator.Validate(operation.OutputSchema, result.Payload); err != nil {
			return InvocationResult{}, fmt.Errorf("connector output schema: %w", err)
		}
	}
	return result, nil
}

func (a *HTTPAdapter) Status(ctx context.Context, invocationID string) (InvocationResult, error) {
	id, err := uuid.Parse(strings.TrimSpace(invocationID))
	if err != nil || id == uuid.Nil {
		return InvocationResult{}, fmt.Errorf("connector invocation id is invalid")
	}
	endpoint := a.descriptor.BaseURL + "/v1/invocations/" + url.PathEscape(id.String())
	result, err := a.do(ctx, http.MethodGet, endpoint, id.String(), nil, MaxResponseBytes)
	if err != nil {
		return InvocationResult{}, err
	}
	operation, ok := a.descriptor.Operation(result.Operation)
	if !ok {
		return InvocationResult{}, fmt.Errorf("connector response operation is not registered")
	}
	if err := validResult(
		result,
		id.String(),
		a.descriptor.ProductID,
		operation.CapabilityID,
		operation.Name,
	); err != nil {
		return InvocationResult{}, err
	}
	if result.Status == "succeeded" {
		if err := a.validator.Validate(operation.OutputSchema, result.Payload); err != nil {
			return InvocationResult{}, fmt.Errorf("connector output schema: %w", err)
		}
	}
	return result, nil
}

func (a *HTTPAdapter) validateInvokeRequest(request InvokeRequest) (OperationDescriptor, error) {
	invocationID, err := uuid.Parse(strings.TrimSpace(request.InvocationID))
	if err != nil || invocationID == uuid.Nil || strings.TrimSpace(request.OrgID) == "" ||
		strings.TrimSpace(request.IdempotencyKey) == "" || request.Arguments == nil {
		return OperationDescriptor{}, fmt.Errorf("connector invocation metadata is invalid")
	}
	if strings.TrimSpace(request.ProductID) != a.descriptor.ProductID {
		return OperationDescriptor{}, fmt.Errorf("connector invocation product does not match descriptor")
	}
	operation, ok := a.descriptor.Operation(request.Operation)
	if !ok || operation.CapabilityID != strings.TrimSpace(request.CapabilityID) {
		return OperationDescriptor{}, fmt.Errorf("connector invocation capability or operation is not registered")
	}
	if err := a.validator.Validate(operation.InputSchema, request.Arguments); err != nil {
		return OperationDescriptor{}, fmt.Errorf("connector input schema: %w", err)
	}
	return operation, nil
}

func (a *HTTPAdapter) do(ctx context.Context, method, endpoint, idempotencyKey string, body []byte, maxResponseBytes int64) (InvocationResult, error) {
	secret, err := a.secrets.Resolve(ctx, a.descriptor.SecretRef)
	if err != nil || len(secret) < 32 {
		return InvocationResult{}, fmt.Errorf("resolve connector signing secret")
	}
	defer zero(secret)
	timestamp := strconv.FormatInt(a.now().Unix(), 10)
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return InvocationResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(headerTimestamp, timestamp)
	request.Header.Set(headerIdempotency, idempotencyKey)
	request.Header.Set(headerSignature, signature(secret, canonicalRequest(method, request.URL.RequestURI(), timestamp, idempotencyKey, body)))
	response, err := a.client.Do(request)
	if err != nil {
		return InvocationResult{}, &TransportError{Ambiguous: method == http.MethodPost, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return InvocationResult{}, &TransportError{Ambiguous: method == http.MethodPost, Cause: err}
	}
	if int64(len(raw)) > maxResponseBytes {
		return InvocationResult{}, fmt.Errorf("connector response exceeds operation limit")
	}
	expected := signature(secret, canonicalResponse(response.StatusCode, idempotencyKey, raw))
	if !hmac.Equal([]byte(expected), []byte(strings.TrimSpace(response.Header.Get(headerResponseSignature)))) {
		return InvocationResult{}, fmt.Errorf("connector response signature is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return InvocationResult{}, &HTTPError{
			StatusCode: response.StatusCode,
			Retryable:  response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500,
		}
	}
	var result InvocationResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return InvocationResult{}, fmt.Errorf("decode connector response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return InvocationResult{}, fmt.Errorf("connector response must contain one JSON object")
	}
	return result, nil
}

type HTTPError struct {
	StatusCode int
	Retryable  bool
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("connector returned HTTP %d", e.StatusCode)
}

type TransportError struct {
	InvocationID string
	Ambiguous    bool
	Cause        error
}

func (e *TransportError) Error() string {
	if e.Cause == nil {
		return "connector transport failed"
	}
	return e.Cause.Error()
}

func (e *TransportError) Unwrap() error { return e.Cause }

func canonicalRequest(method, requestURI, timestamp, idempotencyKey string, body []byte) string {
	return strings.Join([]string{
		strings.ToUpper(method), requestURI, timestamp, idempotencyKey, bodyHash(body),
	}, "\n")
}

func canonicalResponse(statusCode int, idempotencyKey string, body []byte) string {
	return strings.Join([]string{strconv.Itoa(statusCode), idempotencyKey, bodyHash(body)}, "\n")
}

func signature(secret []byte, canonical string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func bodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
