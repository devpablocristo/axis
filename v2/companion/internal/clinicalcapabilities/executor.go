package clinicalcapabilities

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	capabilitydomain "github.com/devpablocristo/companion-v2/internal/capabilities/usecases/domain"
	"github.com/devpablocristo/companion-v2/internal/knowledgebases"
	"github.com/devpablocristo/companion-v2/internal/mcpgovernance"
	"github.com/devpablocristo/companion-v2/internal/virployees"
	"github.com/devpablocristo/companion-v2/internal/virployees/runtimecontext"
	"github.com/devpablocristo/platform/errors/go/domainerr"
	"github.com/google/uuid"
)

const (
	maxExcerptRunes = 1200
	maxCorpusParts  = 200
	maxCorpusChars  = 100000
)

type ScoredRetriever interface {
	Search(context.Context, knowledgebases.RetrievalScope, string, int, int) (knowledgebases.SearchPage, error)
}

type RuntimeContextProvider interface {
	RuntimeContext(context.Context, string, uuid.UUID) (runtimecontext.Context, error)
}

type Executor struct {
	retriever ScoredRetriever
	runtime   RuntimeContextProvider
	answerer  virployees.RuntimeAnswererPort
}

func NewExecutor(retriever ScoredRetriever, runtime RuntimeContextProvider, answerer virployees.RuntimeAnswererPort) *Executor {
	return &Executor{retriever: retriever, runtime: runtime, answerer: answerer}
}

func (e *Executor) Execute(ctx context.Context, invocation mcpgovernance.InvocationContext, capability capabilitydomain.Capability, arguments map[string]any) (map[string]any, error) {
	if capability.SideEffectClass != "read" || capability.RequiresNexusApproval {
		return nil, domainerr.Forbidden("Assist only invokes read-only capabilities")
	}
	if strings.TrimSpace(invocation.ProductSurface) == "" || strings.TrimSpace(invocation.RepositoryGeneration) == "" {
		return nil, domainerr.Validation("product_surface and repository_generation are required")
	}
	timeout := time.Duration(capability.Manifest.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		return nil, domainerr.Conflict("capability timeout is not configured")
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	switch capability.CapabilityKey {
	case RecordsSearchKey:
		return e.search(callCtx, invocation, capability, arguments)
	case TimelineBuildKey:
		return e.timeline(callCtx, invocation, capability, arguments)
	default:
		return nil, domainerr.Conflict("clinical capability executor does not support the requested key")
	}
}

func (e *Executor) retrievalScope(invocation mcpgovernance.InvocationContext) knowledgebases.RetrievalScope {
	return knowledgebases.RetrievalScope{
		OrgID: invocation.OrgID, VirployeeID: invocation.VirployeeID,
		SubjectID: invocation.SubjectID.String(), CaseID: invocation.CaseID,
		ProductSurface: invocation.ProductSurface, RepositoryGeneration: invocation.RepositoryGeneration,
	}
}

func (e *Executor) search(ctx context.Context, invocation mcpgovernance.InvocationContext, capability capabilitydomain.Capability, arguments map[string]any) (map[string]any, error) {
	query, _ := arguments["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" || utf8.RuneCountInString(query) > 4000 {
		return nil, domainerr.Validation("query is required and must not exceed 4000 characters")
	}
	limit, err := integerArgument(arguments, "limit", 20, 1, 50)
	if err != nil {
		return nil, err
	}
	bindingHash, _ := mcpgovernance.Hash(map[string]any{
		"schema_version": "clinical.search.cursor-binding.v1", "org_id": invocation.OrgID,
		"virployee_id": invocation.VirployeeID.String(), "subject_id": invocation.SubjectID.String(),
		"case_id": optionalUUID(invocation.CaseID), "product_surface": invocation.ProductSurface,
		"repository_generation": invocation.RepositoryGeneration, "query": query,
		"manifest_hash": capability.ManifestHash,
	})
	offset := 0
	if raw, _ := arguments["cursor"].(string); strings.TrimSpace(raw) != "" {
		offset, err = decodeCursor(raw, bindingHash)
		if err != nil {
			return nil, err
		}
	}
	page, err := e.retriever.Search(ctx, e.retrievalScope(invocation), query, offset, limit)
	if err != nil {
		return nil, domainerr.Unavailable("clinical index retrieval failed")
	}
	matches := make([]any, 0, len(page.Matches))
	citations := make([]any, 0, len(page.Matches))
	for _, match := range page.Matches {
		reference := referenceMap(match.Citation)
		matches = append(matches, map[string]any{
			"excerpt": boundedRunes(strings.TrimSpace(match.Part.Text), maxExcerptRunes),
			"score":   match.Score, "reference": reference,
		})
		citations = append(citations, reference)
	}
	nextCursor := ""
	if page.HasMore {
		nextCursor = encodeCursor(offset+len(matches), bindingHash)
	}
	warnings := []any{}
	status := "completed"
	if page.Truncated {
		status = "partial"
		warnings = append(warnings, "retrieval_window_truncated")
	}
	return map[string]any{
		"schema_version": "clinical.records.search.v1", "status": status, "query": query,
		"matches": matches, "next_cursor": nextCursor,
		"truncated": page.Truncated || page.HasMore, "warnings": warnings, "citations": citations,
	}, nil
}

type cursorPayload struct {
	Version int    `json:"v"`
	Offset  int    `json:"offset"`
	Binding string `json:"binding"`
}

func encodeCursor(offset int, binding string) string {
	raw, _ := json.Marshal(cursorPayload{Version: 1, Offset: offset, Binding: binding})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(raw, binding string) (int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(decoded) > 4096 {
		return 0, domainerr.Validation("cursor is invalid")
	}
	var payload cursorPayload
	if json.Unmarshal(decoded, &payload) != nil || payload.Version != 1 || payload.Offset < 0 || payload.Offset > 10000 || payload.Binding != binding {
		return 0, domainerr.Validation("cursor does not belong to this clinical search scope")
	}
	return payload.Offset, nil
}

func (e *Executor) timeline(ctx context.Context, invocation mcpgovernance.InvocationContext, capability capabilitydomain.Capability, arguments map[string]any) (map[string]any, error) {
	order, _ := arguments["order"].(string)
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		return nil, domainerr.Validation("order must be asc or desc")
	}
	maxEvents, err := integerArgument(arguments, "max_events", 100, 1, 200)
	if err != nil {
		return nil, err
	}
	dateFrom, fromTime, err := optionalRFC3339(arguments, "date_from")
	if err != nil {
		return nil, err
	}
	dateTo, toTime, err := optionalRFC3339(arguments, "date_to")
	if err != nil {
		return nil, err
	}
	if fromTime != nil && toTime != nil && fromTime.After(*toTime) {
		return nil, domainerr.Validation("date_from must not be after date_to")
	}
	focus, _ := arguments["focus"].(string)
	focus = strings.TrimSpace(focus)
	if utf8.RuneCountInString(focus) > 2000 {
		return nil, domainerr.Validation("focus must not exceed 2000 characters")
	}
	query := "clinical chronology diagnoses treatments procedures medications laboratory results encounters"
	if focus != "" {
		query += " " + focus
	}
	page, err := e.retriever.Search(ctx, e.retrievalScope(invocation), query, 0, maxCorpusParts+1)
	if err != nil {
		return nil, domainerr.Unavailable("clinical corpus retrieval failed")
	}
	parts := make([]knowledgebases.SearchMatch, 0, min(len(page.Matches), maxCorpusParts))
	chars := 0
	corpusTruncated := page.Truncated || len(page.Matches) > maxCorpusParts
	for _, match := range page.Matches {
		if len(parts) >= maxCorpusParts {
			corpusTruncated = true
			break
		}
		textChars := utf8.RuneCountInString(match.Part.Text)
		if chars+textChars > maxCorpusChars {
			remaining := maxCorpusChars - chars
			if remaining > 0 {
				match.Part.Text = boundedRunes(match.Part.Text, remaining)
				parts = append(parts, match)
			}
			corpusTruncated = true
			break
		}
		chars += textChars
		parts = append(parts, match)
	}
	scope := map[string]any{"date_from": dateFrom, "date_to": dateTo, "order": order, "focus": focus}
	if len(parts) == 0 {
		return abstainedTimeline(scope, corpusTruncated, "no_authorized_source_evidence", 0), nil
	}
	if e.runtime == nil || e.answerer == nil {
		return nil, domainerr.Conflict("timeline runtime is not configured")
	}
	rc, err := e.runtime.RuntimeContext(ctx, invocation.OrgID, invocation.VirployeeID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]knowledgebases.Citation, len(parts))
	runtimeParts := make([]knowledgebases.SearchMatch, 0, len(parts))
	for _, match := range parts {
		key := citationKey(match.Citation)
		allowed[key] = match.Citation
		runtimeParts = append(runtimeParts, match)
	}
	request, _ := json.Marshal(map[string]any{
		"task":  "Build a clinical timeline using only supplied sources. Every event must include one or more exact supplied references.",
		"scope": scope, "max_events": maxEvents,
	})
	answerInput := virployees.AnswerInput{
		SystemPrompt: rc.ProfileTemplate.SystemPrompt, JobRole: rc.JobRole.Name,
		ProfessionalContext: virployees.ProfessionalContext{
			JobRoleID: rc.JobRole.ID.String(), Name: rc.JobRole.Name, Mission: rc.JobRole.Mission,
			Responsibilities: rc.JobRole.Responsibilities, SuccessCriteria: rc.JobRole.SuccessCriteria,
		},
		InputJSON: request, ResponseSchema: capability.Manifest.OutputSchema,
		GroundingMode: "sources_only",
	}
	for _, match := range runtimeParts {
		answerInput.ContentParts = append(answerInput.ContentParts, match.Part)
	}
	for attempt := 0; attempt < 2; attempt++ {
		out, answerErr := e.answerer.Answer(ctx, answerInput)
		if answerErr != nil {
			return nil, answerErr
		}
		result, valid := validateAndCanonicalizeTimeline(out.OutputJSON, allowed, scope, order, fromTime, toTime, maxEvents, corpusTruncated)
		valid = valid && out.Answered
		if valid {
			return result, nil
		}
		answerInput.InputJSON, _ = json.Marshal(map[string]any{
			"request": json.RawMessage(request),
			"repair":  "Return the exact response schema and copy only canonical references supplied with the source chunks. Do not emit any unsupported event.",
		})
	}
	return abstainedTimeline(scope, corpusTruncated, "unsupported_or_invalid_citations", sourceCount(allowed)), nil
}

func validateAndCanonicalizeTimeline(raw json.RawMessage, allowed map[string]knowledgebases.Citation, scope map[string]any, order string, from, to *time.Time, maxEvents int, corpusTruncated bool) (map[string]any, bool) {
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, false
	}
	events, ok := result["events"].([]any)
	if !ok {
		return nil, false
	}
	canonical := make([]map[string]any, 0, len(events))
	for _, rawEvent := range events {
		event, ok := rawEvent.(map[string]any)
		if !ok {
			return nil, false
		}
		references, ok := event["references"].([]any)
		if !ok || len(references) == 0 {
			return nil, false
		}
		canonicalReferences := make([]any, 0, len(references))
		for _, rawReference := range references {
			reference, ok := rawReference.(map[string]any)
			if !ok {
				return nil, false
			}
			key := referenceValueKey(reference)
			citation, exists := allowed[key]
			if !exists {
				return nil, false
			}
			canonicalReferences = append(canonicalReferences, referenceMap(citation))
		}
		event["references"] = canonicalReferences
		parsed, hasDate, validDate := parseEventDate(event)
		if !validDate {
			return nil, false
		}
		if hasDate && ((from != nil && parsed.Before(*from)) || (to != nil && parsed.After(*to))) {
			continue
		}
		canonical = append(canonical, event)
	}
	sort.SliceStable(canonical, func(i, j int) bool {
		left, _ := canonical[i]["date"].(string)
		right, _ := canonical[j]["date"].(string)
		if left == right {
			return fmt.Sprint(canonical[i]["title"]) < fmt.Sprint(canonical[j]["title"])
		}
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		if order == "asc" {
			return left < right
		}
		return left > right
	})
	eventLimitTruncated := len(canonical) > maxEvents
	if eventLimitTruncated {
		canonical = canonical[:maxEvents]
	}
	eventsWithoutDate := 0
	outEvents := make([]any, 0, len(canonical))
	for _, event := range canonical {
		if strings.TrimSpace(fmt.Sprint(event["date"])) == "" {
			eventsWithoutDate++
		}
		outEvents = append(outEvents, event)
	}
	warnings := []any{}
	status := "completed"
	if corpusTruncated {
		status = "partial"
		warnings = append(warnings, "corpus_truncated")
	}
	if eventLimitTruncated {
		status = "partial"
		warnings = append(warnings, "event_limit_truncated")
	}
	result = map[string]any{
		"schema_version": "clinical.timeline.build.v1", "status": status, "scope": scope,
		"events": outEvents, "coverage": map[string]any{
			"sources_considered": float64(sourceCount(allowed)), "events_without_date": float64(eventsWithoutDate),
			"corpus_truncated": corpusTruncated, "event_limit_truncated": eventLimitTruncated,
		}, "warnings": warnings, "citations": eventReferences(outEvents),
	}
	if err := mcpgovernance.ValidateJSONSchema(TimelineOutputSchema(), result); err != nil {
		return nil, false
	}
	return result, true
}

func parseEventDate(event map[string]any) (time.Time, bool, bool) {
	date, ok := event["date"].(string)
	if !ok {
		return time.Time{}, false, false
	}
	date = strings.TrimSpace(date)
	precision, ok := event["date_precision"].(string)
	if !ok {
		return time.Time{}, false, false
	}
	if date == "" {
		return time.Time{}, false, precision == "unknown"
	}
	var layout string
	switch precision {
	case "instant":
		layout = time.RFC3339
	case "day":
		layout = "2006-01-02"
	case "month":
		layout = "2006-01"
	case "year":
		layout = "2006"
	default:
		return time.Time{}, false, false
	}
	parsed, err := time.Parse(layout, date)
	return parsed, true, err == nil
}

func abstainedTimeline(scope map[string]any, corpusTruncated bool, reason string, sourcesConsidered int) map[string]any {
	return map[string]any{
		"schema_version": "clinical.timeline.build.v1", "status": "abstained", "scope": scope,
		"events": []any{}, "coverage": map[string]any{
			"sources_considered": float64(sourcesConsidered), "events_without_date": float64(0),
			"corpus_truncated": corpusTruncated, "event_limit_truncated": false,
		}, "warnings": []any{reason}, "citations": []any{},
	}
}

func eventReferences(events []any) []any {
	out := []any{}
	seen := map[string]struct{}{}
	for _, value := range events {
		event, _ := value.(map[string]any)
		references, _ := event["references"].([]any)
		for _, reference := range references {
			raw, _ := json.Marshal(reference)
			key := string(raw)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, reference)
		}
	}
	return out
}

func sourceCount(allowed map[string]knowledgebases.Citation) int {
	documents := make(map[string]struct{}, len(allowed))
	for _, citation := range allowed {
		documents[citation.DocumentID] = struct{}{}
	}
	return len(documents)
}

func integerArgument(arguments map[string]any, key string, fallback, minimum, maximum int) (int, error) {
	value, exists := arguments[key]
	if !exists {
		return fallback, nil
	}
	number, ok := value.(float64)
	integer := int(number)
	if !ok || number != float64(integer) || integer < minimum || integer > maximum {
		return 0, domainerr.Validation(key + " is outside the allowed range")
	}
	return integer, nil
}

func optionalRFC3339(arguments map[string]any, key string) (string, *time.Time, error) {
	raw, exists := arguments[key]
	if !exists {
		return "", nil, nil
	}
	value, ok := raw.(string)
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", nil, domainerr.Validation(key + " must be RFC3339")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", nil, domainerr.Validation(key + " must be RFC3339")
	}
	return value, &parsed, nil
}

func referenceMap(citation knowledgebases.Citation) map[string]any {
	var locator any = map[string]any{}
	if len(citation.Locator) > 0 {
		_ = json.Unmarshal(citation.Locator, &locator)
		if locator == nil {
			locator = map[string]any{}
		}
	}
	return map[string]any{
		"document_id": citation.DocumentID, "source_version": citation.SourceVersion,
		"sha256": citation.SHA256, "locator": locator,
	}
}

func citationKey(citation knowledgebases.Citation) string {
	return citation.DocumentID + "\x00" + citation.SourceVersion + "\x00" + citation.SHA256 + "\x00" + canonicalLocator(citation.Locator)
}

func referenceValueKey(reference map[string]any) string {
	locator, _ := json.Marshal(reference["locator"])
	return fmt.Sprint(reference["document_id"]) + "\x00" + fmt.Sprint(reference["source_version"]) + "\x00" + fmt.Sprint(reference["sha256"]) + "\x00" + canonicalLocator(locator)
}

func canonicalLocator(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	canonical, _ := json.Marshal(value)
	return string(canonical)
}

func boundedRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func optionalUUID(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
