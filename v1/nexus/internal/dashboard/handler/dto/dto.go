package dto

// SummaryResponse representa la respuesta HTTP del dashboard.
type SummaryResponse struct {
	Period          string `json:"period"`
	TotalRequests   int    `json:"total_requests"`
	Allowed         int    `json:"allowed"`
	Denied          int    `json:"denied"`
	PendingApproval int    `json:"pending_approval"`
	Approved        int    `json:"approved"`
	Rejected        int    `json:"rejected"`
}
