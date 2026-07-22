package dryrun

import (
	"regexp"
	"strings"
	"unicode"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	virployeedomain "github.com/devpablocristo/companion-v2/internal/virployees/usecases/domain"
)

type Decision string

const (
	DecisionAllowed Decision = "allowed"
	DecisionBlocked Decision = "blocked"
)

type RequiredCapability struct {
	ID               string
	CapabilityKey    string
	Name             string
	RequiredAutonomy virployeedomain.AutonomyLevel
	Matched          bool
}

type Intent struct {
	Matched       bool
	CapabilityKey string
	Domain        string
	Resource      string
	Action        string
	Confidence    float64
	MatchedBy     []string
	Rules         []IntentRule

	// Provenance of the proposal. The deterministic matcher sets
	// ProposedBy="deterministic"; the LLM runtime sets "llm" plus ModelID and
	// PromptVersion. These are bound into the action binding so a change of
	// model or prompt invalidates prior approvals.
	ProposedBy    string
	ModelID       string
	PromptVersion string
}

// ConfidenceThreshold is the minimum confidence a matched proposal needs to be
// acted on. Below it, the proposal is treated as no intent (conversational),
// so low-confidence guesses never reach governance or execution.
const ConfidenceThreshold = 0.4

type IntentRule struct {
	Type   string
	Target string
	Value  string
}

type DraftStatus string

const (
	DraftStatusReady         DraftStatus = "ready"
	DraftStatusNeedsInput    DraftStatus = "needs_input"
	DraftStatusBlocked       DraftStatus = "blocked"
	DraftStatusNotApplicable DraftStatus = "not_applicable"
)

type Draft struct {
	Status        DraftStatus
	Action        string
	Kind          string
	Summary       string
	Fields        []DraftField
	MissingFields []DraftMissingField
	Notes         []string
}

type DraftField struct {
	Key    string
	Label  string
	Value  string
	Source string
}

type DraftMissingField struct {
	Key    string
	Label  string
	Reason string
}

type Result struct {
	Input              string
	RuntimeContext     runtimecontext.Context
	Intent             Intent
	RequiredCapability *RequiredCapability
	RequiredAutonomy   virployeedomain.AutonomyLevel
	VirployeeAutonomy  virployeedomain.AutonomyLevel
	Decision           Decision
	Reason             string
	NextStep           string
	Draft              Draft
}

type matchedIntent struct {
	Intent
	requiredAutonomy virployeedomain.AutonomyLevel
}

type intentDefinition struct {
	Domain           string
	Resource         string
	Action           string
	CapabilityKey    string
	RequiredAutonomy virployeedomain.AutonomyLevel
	ResourceKeywords []string
	ActionKeywords   []string
}

// Proposal is what a planner (the deterministic matcher today, the LLM runtime
// in Fase 2) proposes for an input: an intent and a fallback required autonomy.
// Go always decides on the proposal; the planner never decides by itself.
type Proposal struct {
	Intent                Intent
	RequiredAutonomy      virployeedomain.AutonomyLevel
	InputTokens           int64
	OutputTokens          int64
	EstimatedCostMicroUSD int64
}

// Evaluate runs the deterministic proposer and then decides on the proposal.
func Evaluate(input string, ctx runtimecontext.Context) Result {
	matched := MatchIntent(strings.TrimSpace(input), ctx.Capabilities)
	intent := matched.Intent
	if intent.Matched {
		intent.ProposedBy = "deterministic"
	}
	return EvaluateWithProposal(input, ctx, Proposal{
		Intent:           intent,
		RequiredAutonomy: matched.requiredAutonomy,
	})
}

// EvaluateWithProposal applies the governance decision to a proposal from any
// planner. Recognition can be replaced (deterministic or LLM), but the decision
// — assignment check, autonomy, draft — stays here in Go.
func EvaluateWithProposal(input string, ctx runtimecontext.Context, proposal Proposal) Result {
	input = strings.TrimSpace(input)
	intent := proposal.Intent
	// Confidence gate: a low-confidence proposal is not acted on. Treat it as no
	// intent so it never reaches capability assignment, governance or execution.
	if intent.Matched && intent.Confidence > 0 && intent.Confidence < ConfidenceThreshold {
		intent.Matched = false
	}
	result := Result{
		Input:             input,
		RuntimeContext:    ctx,
		Intent:            intent,
		VirployeeAutonomy: ctx.Virployee.Autonomy,
	}

	if !intent.Matched {
		result.RequiredAutonomy = virployeedomain.AutonomyA0
		result.Decision = DecisionAllowed
		result.Reason = "no operational capability was inferred from the input"
		result.NextStep = "would answer conversationally using the runtime context"
		result.Draft = buildDraft(input, intent, result.Decision)
		return result
	}

	required := RequiredCapability{
		CapabilityKey:    intent.CapabilityKey,
		RequiredAutonomy: proposal.RequiredAutonomy,
		Matched:          false,
	}
	if matched, ok := findCapability(ctx.Capabilities, intent.CapabilityKey); ok {
		required.ID = matched.ID.String()
		required.Name = matched.Name
		required.RequiredAutonomy = matched.RequiredAutonomy
		required.Matched = true
		result.RequiredAutonomy = matched.RequiredAutonomy
		result.RequiredCapability = &required
		if ctx.Virployee.Autonomy.Allows(matched.RequiredAutonomy) {
			result.Decision = DecisionAllowed
			result.Reason = "virployee autonomy allows the required capability"
			result.NextStep = nextStepFor(matched.RequiredAutonomy)
			result.Draft = buildDraft(input, intent, result.Decision)
			return result
		}
		result.Decision = DecisionBlocked
		result.Reason = "virployee autonomy is lower than the required capability autonomy"
		result.NextStep = "would stop before preparing or executing the action"
		result.Draft = buildDraft(input, intent, result.Decision)
		return result
	}

	result.RequiredAutonomy = proposal.RequiredAutonomy
	result.RequiredCapability = &required
	result.Decision = DecisionBlocked
	result.Reason = "required capability is not assigned to the virployee"
	result.NextStep = "would stop and ask for an assigned capability before proceeding"
	result.Draft = buildDraft(input, intent, result.Decision)
	return result
}

func MatchIntent(input string, capabilities []capabilitydomain.Capability) matchedIntent {
	text := normalizeText(input)
	if text == "" {
		return matchedIntent{Intent: Intent{Rules: []IntentRule{}, MatchedBy: []string{}}}
	}
	definitions := catalogForCapabilities(capabilities)
	var defaultRead *intentDefinition
	var resourceMatch string
	for _, definition := range definitions {
		matchedResource, ok := firstMatchedKeyword(text, definition.ResourceKeywords)
		if !ok {
			continue
		}
		if resourceMatch == "" {
			resourceMatch = matchedResource
		}
		if definition.Action == "read" {
			def := definition
			defaultRead = &def
		}
		matchedAction, ok := firstMatchedKeyword(text, definition.ActionKeywords)
		if !ok {
			continue
		}
		return matchedIntent{
			Intent: Intent{
				Matched:       true,
				CapabilityKey: definition.CapabilityKey,
				Domain:        definition.Domain,
				Resource:      definition.Resource,
				Action:        definition.Action,
				Confidence:    0.9,
				MatchedBy: []string{
					"resource:" + matchedResource,
					"action:" + matchedAction,
				},
				Rules: []IntentRule{
					{Type: "keyword", Target: "resource", Value: matchedResource},
					{Type: "keyword", Target: "action", Value: matchedAction},
				},
			},
			requiredAutonomy: definition.RequiredAutonomy,
		}
	}
	if defaultRead != nil && resourceMatch != "" {
		return matchedIntent{
			Intent: Intent{
				Matched:       true,
				CapabilityKey: defaultRead.CapabilityKey,
				Domain:        defaultRead.Domain,
				Resource:      defaultRead.Resource,
				Action:        defaultRead.Action,
				Confidence:    0.65,
				MatchedBy: []string{
					"resource:" + resourceMatch,
					"action:default:read",
				},
				Rules: []IntentRule{
					{Type: "keyword", Target: "resource", Value: resourceMatch},
					{Type: "default", Target: "action", Value: "read"},
				},
			},
			requiredAutonomy: defaultRead.RequiredAutonomy,
		}
	}
	return matchedIntent{Intent: Intent{Rules: []IntentRule{}, MatchedBy: []string{}}}
}

// knownIntentDefinitions holds the deterministic keyword recognition for known
// capability keys, in canonical action order. The RequiredAutonomy here is only
// a fallback; catalogForCapabilities overrides it with the assigned
// capability's actual required autonomy.
func knownIntentDefinitions() []intentDefinition {
	resourceKeywords := []string{
		"calendar",
		"calendario",
		"event",
		"events",
		"evento",
		"eventos",
		"reunion",
		"reuniones",
		"meeting",
		"meetings",
	}
	return []intentDefinition{
		{
			Domain:           "calendar",
			Resource:         "events",
			Action:           "create",
			CapabilityKey:    "calendar.events.create",
			RequiredAutonomy: virployeedomain.AutonomyA2,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"crear", "crea", "create", "agendar", "agenda", "agende", "programar", "programa", "schedule", "book"},
		},
		{
			Domain:           "calendar",
			Resource:         "events",
			Action:           "read",
			CapabilityKey:    "calendar.events.read",
			RequiredAutonomy: virployeedomain.AutonomyA1,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"leer", "lee", "ver", "listar", "lista", "mostrar", "mostra", "consultar", "consulta", "read", "list", "show", "find", "buscar", "busca", "que", "tengo", "hay"},
		},
		{
			Domain:           "calendar",
			Resource:         "events",
			Action:           "update",
			CapabilityKey:    "calendar.events.update",
			RequiredAutonomy: virployeedomain.AutonomyA2,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"editar", "edita", "actualizar", "actualiza", "modificar", "modifica", "cambiar", "cambia", "reprogramar", "reprograma", "update", "change", "reschedule"},
		},
		{
			Domain:           "calendar",
			Resource:         "events",
			Action:           "delete",
			CapabilityKey:    "calendar.events.delete",
			RequiredAutonomy: virployeedomain.AutonomyA2,
			ResourceKeywords: resourceKeywords,
			ActionKeywords:   []string{"eliminar", "elimina", "borrar", "borra", "cancelar", "cancela", "delete", "remove", "cancel"},
		},
	}
}

// catalogForCapabilities builds the intent catalog from ONLY the capabilities
// assigned to the virployee (data-driven per tenant, replacing the former
// global literal). An action the virployee does not have assigned is not
// recognizable, so the deterministic matcher can never infer an intent for an
// unassigned capability. This is the Fase 1 gate that scopes what the runtime
// can act on; the Fase 2 LLM proposer consumes the same assigned-only set.
func catalogForCapabilities(capabilities []capabilitydomain.Capability) []intentDefinition {
	assigned := make(map[string]capabilitydomain.Capability, len(capabilities))
	order := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if _, exists := assigned[capability.CapabilityKey]; exists {
			continue
		}
		assigned[capability.CapabilityKey] = capability
		order = append(order, capability.CapabilityKey)
	}

	known := knownIntentDefinitions()
	knownKeys := make(map[string]bool, len(known))
	out := make([]intentDefinition, 0, len(capabilities))
	// Curated keyword definitions first, in canonical order, only when assigned.
	for _, definition := range known {
		knownKeys[definition.CapabilityKey] = true
		if capability, ok := assigned[definition.CapabilityKey]; ok {
			definition.RequiredAutonomy = capability.RequiredAutonomy
			out = append(out, definition)
		}
	}
	// Assigned capabilities without a curated definition get a derived one so
	// they remain recognizable by their capability_key segments.
	for _, key := range order {
		if knownKeys[key] {
			continue
		}
		if definition, ok := deriveIntentDefinition(assigned[key]); ok {
			out = append(out, definition)
		}
	}
	return out
}

// deriveIntentDefinition builds a minimal keyword definition from a
// domain.resource.action capability key for capabilities without curated
// keywords.
func deriveIntentDefinition(capability capabilitydomain.Capability) (intentDefinition, bool) {
	parts := strings.Split(capability.CapabilityKey, ".")
	if len(parts) != 3 {
		return intentDefinition{}, false
	}
	domain, resource, action := parts[0], parts[1], parts[2]
	resourceKeywords := []string{resource}
	if domain != resource {
		resourceKeywords = append(resourceKeywords, domain)
	}
	return intentDefinition{
		Domain:           domain,
		Resource:         resource,
		Action:           action,
		CapabilityKey:    capability.CapabilityKey,
		RequiredAutonomy: capability.RequiredAutonomy,
		ResourceKeywords: resourceKeywords,
		ActionKeywords:   []string{action},
	}, true
}

func buildDraft(input string, intent Intent, decision Decision) Draft {
	if !intent.Matched {
		return Draft{
			Status:  DraftStatusNotApplicable,
			Summary: "No operational draft was prepared.",
			Fields:  []DraftField{},
			Notes:   []string{"No external action will be executed."},
		}
	}
	switch intent.CapabilityKey {
	case "calendar.events.create":
		return buildCalendarEventCreateDraft(input, intent, decision)
	case "calendar.events.read":
		return buildCalendarEventReadDraft(input, intent, decision)
	case "calendar.events.update":
		return buildCalendarEventUpdateDraft(input, intent, decision)
	case "calendar.events.delete":
		return buildCalendarEventDeleteDraft(input, intent, decision)
	default:
		status := DraftStatusNotApplicable
		if decision == DecisionBlocked {
			status = DraftStatusBlocked
		}
		return Draft{
			Status:  status,
			Action:  intent.CapabilityKey,
			Kind:    "generic_action",
			Summary: "No structured draft is available for this action yet.",
			Fields:  []DraftField{},
			Notes:   []string{"No external action will be executed."},
		}
	}
}

func buildCalendarEventCreateDraft(input string, intent Intent, decision Decision) Draft {
	fields := []DraftField{}
	missing := []DraftMissingField{}

	if title, source := calendarEventTitle(input); title != "" {
		fields = append(fields, DraftField{Key: "title", Label: "Title", Value: title, Source: source})
	} else {
		missing = append(missing, DraftMissingField{Key: "title", Label: "Title", Reason: "Title is required before preparing the event."})
	}
	if dateHint := calendarEventDateHint(input); dateHint != "" {
		fields = append(fields, DraftField{Key: "date_hint", Label: "Date", Value: dateHint, Source: "input"})
	} else {
		missing = append(missing, DraftMissingField{Key: "date_hint", Label: "Date", Reason: "Date is required before preparing the event."})
	}
	if timeValue := calendarEventTime(input); timeValue != "" {
		fields = append(fields, DraftField{Key: "time", Label: "Time", Value: timeValue, Source: "input"})
	} else {
		missing = append(missing, DraftMissingField{Key: "time", Label: "Time", Reason: "Time is required before preparing the event."})
	}
	if attendees := calendarEventAttendees(input); attendees != "" {
		fields = append(fields, DraftField{Key: "attendees", Label: "Attendees", Value: attendees, Source: "input"})
	} else {
		missing = append(missing, DraftMissingField{Key: "attendees", Label: "Attendees", Reason: "At least one attendee is required for a meeting."})
	}

	return Draft{
		Status:        draftStatus(decision, missing),
		Action:        intent.CapabilityKey,
		Kind:          "calendar_event",
		Summary:       "Prepare a calendar event draft",
		Fields:        fields,
		MissingFields: missing,
		Notes:         []string{"No external action will be executed."},
	}
}

func buildCalendarEventReadDraft(input string, intent Intent, decision Decision) Draft {
	fields := []DraftField{{Key: "search_text", Label: "Search text", Value: input, Source: "input"}}
	if dateHint := calendarEventDateHint(input); dateHint != "" {
		fields = append(fields, DraftField{Key: "date_hint", Label: "Date", Value: dateHint, Source: "input"})
	}
	return Draft{
		Status:  draftStatus(decision, nil),
		Action:  intent.CapabilityKey,
		Kind:    "calendar_event_query",
		Summary: "Prepare a calendar event query",
		Fields:  fields,
		Notes:   []string{"No external action will be executed."},
	}
}

func buildCalendarEventUpdateDraft(input string, intent Intent, decision Decision) Draft {
	fields := []DraftField{}
	if title, source := calendarEventTitle(input); title != "" {
		fields = append(fields, DraftField{Key: "title", Label: "Title", Value: title, Source: source})
	}
	if dateHint := calendarEventDateHint(input); dateHint != "" {
		fields = append(fields, DraftField{Key: "date_hint", Label: "Date", Value: dateHint, Source: "input"})
	}
	if timeValue := calendarEventTime(input); timeValue != "" {
		fields = append(fields, DraftField{Key: "time", Label: "Time", Value: timeValue, Source: "input"})
	}
	missing := []DraftMissingField{
		{Key: "event_reference", Label: "Event reference", Reason: "The event to update must be identified before preparing changes."},
		{Key: "changes", Label: "Changes", Reason: "The requested changes must be explicit before preparing the update."},
	}
	return Draft{
		Status:        draftStatus(decision, missing),
		Action:        intent.CapabilityKey,
		Kind:          "calendar_event_update",
		Summary:       "Prepare a calendar event update draft",
		Fields:        fields,
		MissingFields: missing,
		Notes:         []string{"No external action will be executed."},
	}
}

func buildCalendarEventDeleteDraft(input string, intent Intent, decision Decision) Draft {
	fields := []DraftField{}
	if title, source := calendarEventTitle(input); title != "" {
		fields = append(fields, DraftField{Key: "title", Label: "Title", Value: title, Source: source})
	}
	if dateHint := calendarEventDateHint(input); dateHint != "" {
		fields = append(fields, DraftField{Key: "date_hint", Label: "Date", Value: dateHint, Source: "input"})
	}
	missing := []DraftMissingField{
		{Key: "event_reference", Label: "Event reference", Reason: "The event to delete must be identified before preparing deletion."},
	}
	return Draft{
		Status:        draftStatus(decision, missing),
		Action:        intent.CapabilityKey,
		Kind:          "calendar_event_delete",
		Summary:       "Prepare a calendar event deletion draft",
		Fields:        fields,
		MissingFields: missing,
		Notes:         []string{"No external action will be executed."},
	}
}

func draftStatus(decision Decision, missing []DraftMissingField) DraftStatus {
	if decision == DecisionBlocked {
		return DraftStatusBlocked
	}
	if len(missing) > 0 {
		return DraftStatusNeedsInput
	}
	return DraftStatusReady
}

func calendarEventTitle(input string) (string, string) {
	for _, expr := range []*regexp.Regexp{
		regexp.MustCompile(`"([^"]+)"`),
		regexp.MustCompile(`'([^']+)'`),
		regexp.MustCompile(`“([^”]+)”`),
	} {
		if match := expr.FindStringSubmatch(input); len(match) == 2 {
			if title := strings.TrimSpace(match[1]); title != "" {
				return title, "input"
			}
		}
	}
	text := normalizeText(input)
	if matchKeyword(text, "reunion") || matchKeyword(text, "reuniones") || matchKeyword(text, "meeting") || matchKeyword(text, "meetings") {
		return "Reunión", "inferred"
	}
	if matchKeyword(text, "evento") || matchKeyword(text, "event") {
		return "Evento", "inferred"
	}
	return "", ""
}

func calendarEventDateHint(input string) string {
	text := normalizeText(input)
	switch {
	case strings.Contains(text, "pasado manana"):
		return "pasado mañana"
	case matchKeyword(text, "manana"), matchKeyword(text, "tomorrow"):
		return "mañana"
	case matchKeyword(text, "hoy"), matchKeyword(text, "today"):
		return "hoy"
	}
	for _, day := range []struct {
		normalized string
		value      string
	}{
		{"lunes", "lunes"},
		{"martes", "martes"},
		{"miercoles", "miércoles"},
		{"jueves", "jueves"},
		{"viernes", "viernes"},
		{"sabado", "sábado"},
		{"domingo", "domingo"},
		{"monday", "monday"},
		{"tuesday", "tuesday"},
		{"wednesday", "wednesday"},
		{"thursday", "thursday"},
		{"friday", "friday"},
		{"saturday", "saturday"},
		{"sunday", "sunday"},
	} {
		if matchKeyword(text, day.normalized) {
			return day.value
		}
	}
	return ""
}

func calendarEventTime(input string) string {
	for _, expr := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b([01]?\d|2[0-3]):([0-5]\d)\b`),
		regexp.MustCompile(`(?i)\b(?:a las|at)\s+([01]?\d|2[0-3])(?:\s*(?:hs|h))?\b`),
		regexp.MustCompile(`(?i)\b([01]?\d|2[0-3])\s*(?:hs|h)\b`),
		regexp.MustCompile(`(?i)\b(1[0-2]|0?[1-9])\s*(am|pm)\b`),
	} {
		if value := expr.FindString(input); value != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func calendarEventAttendees(input string) string {
	matches := regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`).FindAllString(input, -1)
	if len(matches) == 0 {
		return ""
	}
	seen := map[string]bool{}
	out := []string{}
	for _, match := range matches {
		email := strings.ToLower(match)
		if seen[email] {
			continue
		}
		seen[email] = true
		out = append(out, email)
	}
	return strings.Join(out, ", ")
}

func nextStepFor(required virployeedomain.AutonomyLevel) string {
	switch required {
	case virployeedomain.AutonomyA0:
		return "would answer conversationally using the runtime context"
	case virployeedomain.AutonomyA1:
		return "would analyze and recommend a next action"
	case virployeedomain.AutonomyA2:
		return "would draft or prepare the action without external side effects"
	case virployeedomain.AutonomyA3:
		return "would prepare a limited execution step"
	case virployeedomain.AutonomyA4:
		return "would require governed execution controls before proceeding"
	case virployeedomain.AutonomyA5:
		return "would require broad-autonomy controls before proceeding"
	default:
		return "would stop before proceeding"
	}
}

func findCapability(items []capabilitydomain.Capability, key string) (capabilitydomain.Capability, bool) {
	for _, item := range items {
		if item.CapabilityKey == key {
			return item, true
		}
	}
	return capabilitydomain.Capability{}, false
}

func firstMatchedKeyword(text string, keywords []string) (string, bool) {
	for _, keyword := range keywords {
		normalized := normalizeText(keyword)
		if matchKeyword(text, normalized) {
			return normalized, true
		}
	}
	return "", false
}

func matchKeyword(text string, keyword string) bool {
	keyword = normalizeText(keyword)
	if keyword == "" {
		return false
	}
	if strings.Contains(keyword, " ") {
		return strings.Contains(text, keyword)
	}
	for _, token := range tokenize(text) {
		if token == keyword {
			return true
		}
	}
	return false
}

func tokenize(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func normalizeText(value string) string {
	value = strings.ToLower(value)
	replacements := strings.NewReplacer(
		"á", "a",
		"à", "a",
		"ä", "a",
		"â", "a",
		"é", "e",
		"è", "e",
		"ë", "e",
		"ê", "e",
		"í", "i",
		"ì", "i",
		"ï", "i",
		"î", "i",
		"ó", "o",
		"ò", "o",
		"ö", "o",
		"ô", "o",
		"ú", "u",
		"ù", "u",
		"ü", "u",
		"û", "u",
		"ñ", "n",
	)
	return replacements.Replace(value)
}
