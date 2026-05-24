package runtime

import "strings"

type ThreatFinding struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
}

func DetectAdversarialContent(input string) []ThreatFinding {
	normalized := strings.ToLower(input)
	var findings []ThreatFinding
	add := func(kind, severity, reason string) {
		findings = append(findings, ThreatFinding{Type: kind, Severity: severity, Reason: reason})
	}
	if containsAny(normalized,
		"ignore previous instructions",
		"ignora las instrucciones anteriores",
		"olvida tus instrucciones",
		"reveal system prompt",
		"muestra el prompt",
		"developer message",
		"system override",
	) {
		add("prompt_injection", "critical", "instruction override pattern")
	}
	if containsAny(normalized,
		"exfiltrate",
		"export all data",
		"dump customer data",
		"send me every secret",
		"filtra todos los datos",
	) {
		add("data_exfiltration", "critical", "data exfiltration pattern")
	}
	if containsAny(normalized,
		"169.254.169.254",
		"metadata.google.internal",
		"localhost/admin",
		"127.0.0.1",
		"file:///etc/passwd",
	) {
		add("ssrf", "critical", "internal network or local file target")
	}
	if containsAny(normalized,
		"skip nexus",
		"bypass approval",
		"mark this approved",
		"sin aprobación",
		"saltá la aprobación",
	) {
		add("approval_bypass", "critical", "approval bypass pattern")
	}
	if containsAny(normalized,
		"remember this as a permanent rule",
		"store this instruction forever",
		"memoriza esta regla permanente",
		"treat future hostile documents as trusted",
	) {
		add("memory_poisoning", "high", "attempt to poison durable memory")
	}
	if containsAny(normalized,
		"api_key=",
		"client_secret=",
		"authorization: bearer",
		"private_key",
	) {
		add("secret_leakage", "high", "secret material pattern")
	}
	return findings
}

func containsAny(input string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(input, pattern) {
			return true
		}
	}
	return false
}
