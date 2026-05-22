package dto

// ReplayResponse representa la respuesta HTTP del replay.
type ReplayResponse struct {
	RequestID     string          `json:"request_id"`
	OrgID         string          `json:"org_id,omitempty"`
	Requester     RequesterInfo   `json:"requester"`
	ActionType    string          `json:"action_type"`
	Target        string          `json:"target"`
	FinalStatus   string          `json:"final_status"`
	DurationTotal string          `json:"duration_total,omitempty"`
	Timeline      []TimelineEntry `json:"timeline"`
}

// RequesterInfo representa el solicitante en la respuesta.
type RequesterInfo struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// TimelineEntry representa un evento en la línea de tiempo.
type TimelineEntry struct {
	Event   string `json:"event"`
	Actor   string `json:"actor"`
	At      string `json:"at"`
	Summary string `json:"summary"`
}
