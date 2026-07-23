package productintegrations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestHTTPServiceClientClassifiesPermanentAndRetryableFailures(t *testing.T) {
	for _, test := range []struct {
		status    int
		retryable bool
	}{
		{status: http.StatusBadRequest, retryable: false},
		{status: http.StatusRequestTimeout, retryable: true},
		{status: http.StatusTooManyRequests, retryable: true},
		{status: http.StatusServiceUnavailable, retryable: true},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
			}))
			defer server.Close()
			client := NewHTTPServiceClient("token", server.Client())
			_, err := client.Readiness(context.Background(), ServiceReadinessInput{
				BaseURL: server.URL, OrgID: uuid.New(), ProductID: uuid.New(),
			})
			var participantErr *ParticipantHTTPError
			if !errors.As(err, &participantErr) || participantErr.Retryable != test.retryable {
				t.Fatalf("status %d classification: err=%v retryable=%v", test.status, err, participantErr != nil && participantErr.Retryable)
			}
		})
	}
}
