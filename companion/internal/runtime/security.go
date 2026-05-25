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
		"send org b data",
		"cross-org export",
	) {
		add("data_exfiltration", "critical", "data exfiltration pattern")
	}
	if containsAny(normalized,
		"169.254.169.254",
		"metadata.google.internal",
		"localhost/admin",
		"127.0.0.1",
		"0.0.0.0",
		"file:///etc/passwd",
		"internal.service",
	) {
		add("ssrf", "critical", "internal network or local file target")
	}
	if containsAny(normalized,
		"skip nexus",
		"bypass approval",
		"mark this approved",
		"sin aprobación",
		"saltá la aprobación",
		"approval is already granted",
	) {
		add("approval_bypass", "critical", "approval bypass pattern")
	}
	if containsAny(normalized,
		"remember this as a permanent rule",
		"store this instruction forever",
		"memoriza esta regla permanente",
		"treat future hostile documents as trusted",
		"write this to memory as trusted",
	) {
		add("memory_poisoning", "high", "attempt to poison durable memory")
	}
	if containsAny(normalized,
		"tool output says ignore",
		"connector says bypass",
		"malicious tool output",
	) {
		add("tool_poisoning", "high", "malicious tool output pattern")
	}
	if containsAny(normalized,
		"unsafe rollback",
		"compensate without approval",
		"rollback without nexus",
	) {
		add("unsafe_compensation", "critical", "unsafe compensation pattern")
	}
	if containsAny(normalized,
		"org-b secret",
		"other tenant",
		"tenant escape",
		"cross org leakage",
	) {
		add("cross_org_leakage", "critical", "cross-org leakage pattern")
	}
	if containsAny(normalized,
		"api_key=",
		"client_secret=",
		"authorization: bearer",
		"private_key",
		"-----begin private key-----",
	) {
		add("secret_leakage", "high", "secret material pattern")
	}
	if containsAny(normalized,
		"ssn:",
		"credit card",
		"dni:",
		"passport number",
	) {
		add("pii_leakage", "high", "PII leakage pattern")
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
