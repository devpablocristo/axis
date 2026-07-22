package domain

import "time"

// EvidencePack is the exportable, signed proof of what a virployee did. A third
// party (auditor, regulator) reverifies the signature over the whole pack to
// prove it is authentic and unaltered. The integrity block proves the underlying
// audit chain itself was intact when the pack was generated.
type EvidencePack struct {
	Version      string          `json:"version"`
	GeneratedAt  time.Time       `json:"generated_at"`
	Scope        string          `json:"scope"`
	Virployee    VirployeeRef    `json:"virployee"`
	Subject      *SubjectRef     `json:"subject,omitempty"`
	EventCount   int             `json:"event_count"`
	Timeline     []TimelineEvent `json:"timeline"`
	Integrity    Integrity       `json:"integrity"`
	LinkedChains []LinkedChain   `json:"linked_chains,omitempty"`
	Signature    Signature       `json:"signature"`
}

// LinkedChain proves a second virployee's contribution to the same subject.
// Each chain retains its own integrity root; chains are never flattened into a
// synthetic hash sequence.
type LinkedChain struct {
	Scope       string          `json:"scope"`
	VirployeeID string          `json:"virployee_id"`
	EventCount  int             `json:"event_count"`
	Timeline    []TimelineEvent `json:"timeline"`
	Integrity   Integrity       `json:"integrity"`
}

// VirployeeRef identifies the virtual employee whose ledger this pack covers.
type VirployeeRef struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
}

// SubjectRef is set when the pack is focused on a single subject (e.g. one
// diagnosis run) rather than the whole ledger.
type SubjectRef struct {
	ID              string `json:"id"`
	ChainEventCount int    `json:"chain_event_count"`
}

// TimelineEvent is one entry as it appears in the pack.
type TimelineEvent struct {
	Event     string         `json:"event"`
	Actor     string         `json:"actor"`
	Subject   string         `json:"subject,omitempty"`
	At        string         `json:"at"`
	Summary   string         `json:"summary"`
	Data      map[string]any `json:"data,omitempty"`
	EventHash string         `json:"event_hash"`
}

// Integrity is the verification result of the underlying audit chain.
type Integrity struct {
	Status        string `json:"status"`
	CheckedEvents int    `json:"checked_events"`
	FirstHash     string `json:"first_hash,omitempty"`
	LastHash      string `json:"last_hash,omitempty"`
	Signed        bool   `json:"signed"`
	Error         string `json:"error,omitempty"`
}

// Signature is the cryptographic signature over the pack. Algorithm is "none"
// when no signing key is configured (local-first).
type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id,omitempty"`
	SignedAt  string `json:"signed_at,omitempty"`
	Value     string `json:"value,omitempty"`
}
