package config

import "testing"

func TestKnowledgeUploadBodyLimitDefault(t *testing.T) {
	t.Setenv("BFF_V2_KNOWLEDGE_UPLOAD_MAX_BODY_BYTES", "")
	config := Load()
	if config.MaxBodyBytes != 1<<20 {
		t.Fatalf("default body max=%d", config.MaxBodyBytes)
	}
	if config.KnowledgeUploadMaxBodyBytes != 251<<20 {
		t.Fatalf("knowledge upload max=%d", config.KnowledgeUploadMaxBodyBytes)
	}
}
