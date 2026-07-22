package enterpriseops

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type staticNotificationResolver struct{ destination []byte }

func (r staticNotificationResolver) ResolveNotificationDestination(context.Context, string) ([]byte, error) {
	return append([]byte(nil), r.destination...), nil
}

type recordingNotificationSender struct{ payloads []json.RawMessage }

func (s *recordingNotificationSender) SendNotification(_ context.Context, _ []byte, payload json.RawMessage) error {
	s.payloads = append(s.payloads, append(json.RawMessage(nil), payload...))
	return nil
}

func TestNotificationOutboxDeliveryIsDurableAndMetadataOnly(t *testing.T) {
	databaseURL := os.Getenv("NEXUS_V2_JOBS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEXUS_V2_JOBS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tenant := "notifications-test-" + uuid.NewString()
	incidentID, outboxID := uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM operational_notification_outbox WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(context.Background(), `DELETE FROM operational_incident_events WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(context.Background(), `DELETE FROM operational_incidents WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(context.Background(), `DELETE FROM operational_notification_policy WHERE tenant_id=$1`, tenant)
	})
	if _, err = pool.Exec(ctx, `INSERT INTO operational_notification_policy(tenant_id,enabled,webhook_secret_ref,changed_by)VALUES($1,true,'env://TEST_WEBHOOK','test')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO operational_incidents(id,tenant_id,fingerprint,source,incident_type,resource_type,resource_id,severity)VALUES($1,$2,$3,'test','job.dead_letter','job',$4,'high')`, incidentID, tenant, hash(tenant+incidentID.String()), incidentID.String()); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"incident_id":"` + incidentID.String() + `","severity":"high","incident_type":"job.dead_letter"}`)
	if _, err = pool.Exec(ctx, `INSERT INTO operational_notification_outbox(id,tenant_id,incident_id,event_type,dedupe_key,payload_json)VALUES($1,$2,$3,'opened',$4,$5)`, outboxID, tenant, incidentID, uuid.NewString(), payload); err != nil {
		t.Fatal(err)
	}
	sender := &recordingNotificationSender{}
	service := NewService(pool, nil, nil)
	service.ConfigureNotificationDelivery(staticNotificationResolver{destination: []byte("https://example.test/hook")}, sender)
	delivered, err := service.DeliverNotifications(ctx, 10)
	if err != nil || delivered != 1 || len(sender.payloads) != 1 {
		t.Fatalf("delivered=%d payloads=%d err=%v", delivered, len(sender.payloads), err)
	}
	var status, errorCode string
	if err = pool.QueryRow(ctx, `SELECT status,last_error_code FROM operational_notification_outbox WHERE tenant_id=$1 AND id=$2`, tenant, outboxID).Scan(&status, &errorCode); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" || errorCode != "" {
		t.Fatalf("unexpected outbox state status=%s error=%s", status, errorCode)
	}
	var decoded map[string]any
	if json.Unmarshal(sender.payloads[0], &decoded) != nil || decoded["severity"] != "high" || decoded["incident_type"] != "job.dead_letter" {
		t.Fatalf("unexpected notification payload: %s", sender.payloads[0])
	}
}
