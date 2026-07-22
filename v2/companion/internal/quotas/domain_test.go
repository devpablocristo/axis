package quotas

import (
	"errors"
	"testing"
)

func TestRetryAfterOnlyRecognizesQuotaErrors(t *testing.T) {
	if _, ok := RetryAfter(errors.New("other")); ok {
		t.Fatal("ordinary errors must not be exposed as quota denials")
	}
	seconds, ok := RetryAfter(&ExceededError{RetryAfter: 7})
	if !ok || seconds != 7 {
		t.Fatalf("unexpected retry-after: %d %v", seconds, ok)
	}
}

func TestPolicyValidationRequiresBoundedPositiveLimits(t *testing.T) {
	valid, err := validatePolicy(Policy{
		Key:           Key{OrgID: "organization", ProductSurface: "ProductA", Area: "Inbound"},
		WindowSeconds: 60, RequestLimit: 10, UnitLimit: 100, Active: true,
	})
	if err != nil || valid.ProductSurface != "producta" || valid.Area != "inbound" {
		t.Fatalf("valid policy rejected or not normalized: %+v err=%v", valid, err)
	}
	if _, err := validatePolicy(Policy{Key: valid.Key, WindowSeconds: 0, RequestLimit: 1, UnitLimit: 1}); err == nil {
		t.Fatal("zero window must be rejected")
	}
}
