package dto

// ProposeResponse es la respuesta de POST /companion/v1/nexus-assist/propose.
// Reporta cuántas propuestas se generaron y persistieron en Nexus.
type ProposeResponse struct {
	PatternsAnalyzed   int      `json:"patterns_analyzed"`
	ProposalsSubmitted int      `json:"proposals_submitted"`
	Errors             []string `json:"errors,omitempty"`
}

// ExplainResponse es la respuesta de GET /companion/v1/nexus-assist/explain/{request_id}.
// El summary es para humanos que aprueban; degraded queda reservado para degradaciones explícitas.
type ExplainResponse struct {
	RequestID string `json:"request_id"`
	Summary   string `json:"summary"`
	Degraded  bool   `json:"degraded"`
}
