package enterpriseops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPNotificationSenderDeliversMetadata(t *testing.T) {
	var got map[string]any
	var handlerErr error
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			handlerErr = fmt.Errorf("unexpected notification request: %s %s", request.Method, request.Header.Get("Content-Type"))
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&got); err != nil {
			handlerErr = err
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := NewHTTPNotificationSender(server.Client(), true)
	payload := json.RawMessage(`{"incident_id":"7e0a9d68-9584-4acb-b3c6-7d59e11ffea2","severity":"critical"}`)
	if err := sender.SendNotification(context.Background(), []byte(server.URL), payload); err != nil {
		t.Fatal(err)
	}
	if handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if got["severity"] != "critical" || got["incident_id"] == "" {
		t.Fatalf("unexpected metadata payload: %#v", got)
	}
}

func TestHTTPNotificationSenderRequiresHTTPS(t *testing.T) {
	sender := NewHTTPNotificationSender(http.DefaultClient, false)
	if err := sender.SendNotification(context.Background(), []byte("http://example.test/webhook"), json.RawMessage(`{}`)); err == nil {
		t.Fatal("production notification delivery must reject plaintext HTTP")
	}
}
