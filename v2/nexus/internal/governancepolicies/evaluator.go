package governancepolicies

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

type EvaluationRecorder interface {
	RecordEvaluation(context.Context, Evaluation) error
}

type Evaluator struct {
	env      *cel.Env
	envErr   error
	programs map[string]cel.Program
	mu       sync.RWMutex
	recorder EvaluationRecorder
}

func NewEvaluator(recorder EvaluationRecorder) *Evaluator {
	env, err := cel.NewEnv(
		cel.Variable("action", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("resource", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("product", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("requester", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("authority", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("time", cel.MapType(cel.StringType, cel.DynType)),
	)
	return &Evaluator{env: env, envErr: err, programs: map[string]cel.Program{}, recorder: recorder}
}

func (e *Evaluator) Validate(expression string) error {
	_, err := e.program(expression)
	return err
}

func (e *Evaluator) Evaluate(ctx context.Context, orgID string, versions []Version, in SafeInput) (EvaluationResult, error) {
	if in.Now.IsZero() {
		return EvaluationResult{}, fmt.Errorf("policy evaluation requires a trusted time")
	}
	sort.SliceStable(versions, func(i, j int) bool {
		if versions[i].Priority == versions[j].Priority {
			return versions[i].ID.String() < versions[j].ID.String()
		}
		return versions[i].Priority < versions[j].Priority
	})
	snapshot := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		// Only enforced versions applicable to this context bind approvals.
		// Shadow policies still emit evaluations and matches, but must not revoke
		// otherwise valid approvals merely because an experiment changed.
		if version.State == StateActive && version.AppliesTo(in) {
			snapshot = append(snapshot, map[string]any{"id": version.ID, "content_hash": version.ContentHash, "state": version.State})
		}
	}
	result := EvaluationResult{EffectiveRisk: normalizeRisk(in.RiskClass), PolicySnapshotHash: Hash(snapshot), InputHash: Hash(in), Matches: []PolicyMatch{}}
	activation := safeActivation(in)
	matchedEnforced := make([]Version, 0)
	for _, version := range versions {
		if !version.AppliesTo(in) {
			continue
		}
		matched, err := e.matches(version.Expression, activation)
		mode := "enforced"
		if version.State == StateShadow {
			mode = "shadow"
		}
		evaluation := Evaluation{OrgID: orgID, PolicyVersionID: version.ID, Mode: mode, Matched: matched, Effect: version.Effect, InputHash: result.InputHash, ErrorCode: errorCode(err)}
		if err != nil {
			if mode == "shadow" {
				e.recordBestEffort(ctx, evaluation)
				continue
			}
			_ = e.record(ctx, evaluation)
			return EvaluationResult{}, fmt.Errorf("enforced policy %s failed closed: %w", version.ID, err)
		}
		if !matched {
			if mode == "shadow" {
				e.recordBestEffort(ctx, evaluation)
			} else if err := e.record(ctx, evaluation); err != nil {
				return EvaluationResult{}, err
			}
			continue
		}
		match := PolicyMatch{PolicyID: version.PolicyID, VersionID: version.ID, Version: version.Version, Effect: version.Effect, Mode: mode, Priority: version.Priority, RiskOverride: version.RiskOverride, ContentHash: version.ContentHash, ExpressionTrue: true}
		result.Matches = append(result.Matches, match)
		evaluation.Decision = version.Effect
		if mode == "shadow" {
			e.recordBestEffort(ctx, evaluation)
			continue
		}
		if err := e.record(ctx, evaluation); err != nil {
			return EvaluationResult{}, err
		}
		matchedEnforced = append(matchedEnforced, version)
		result.EffectiveRisk = RaiseRisk(result.EffectiveRisk, version.RiskOverride)
	}
	if len(matchedEnforced) == 0 {
		result.Reason = "no enforced policy matched"
		return result, nil
	}
	result.Matched = true
	result.Decision = precedenceDecision(matchedEnforced)
	if result.Decision == EffectAllow && RiskRequiresApproval(result.EffectiveRisk) {
		result.Decision = EffectRequireApproval
		result.Reason = "allow cannot bypass approval for high or critical risk"
	} else {
		result.Reason = "effective policy decision: " + result.Decision
	}
	return result, nil
}

func (e *Evaluator) MatchOne(version Version, in SafeInput) (bool, error) {
	if !version.AppliesTo(in) {
		return false, nil
	}
	return e.matches(version.Expression, safeActivation(in))
}

func (e *Evaluator) matches(expression string, activation map[string]any) (bool, error) {
	program, err := e.program(expression)
	if err != nil {
		return false, err
	}
	value, _, err := program.Eval(activation)
	if err != nil {
		return false, fmt.Errorf("evaluate CEL: %w", err)
	}
	if value.Type() != types.BoolType {
		return false, fmt.Errorf("CEL expression must return bool")
	}
	matched, ok := value.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL result is not boolean")
	}
	return matched, nil
}

func (e *Evaluator) program(expression string) (cel.Program, error) {
	if e.envErr != nil {
		return nil, e.envErr
	}
	e.mu.RLock()
	program, ok := e.programs[expression]
	e.mu.RUnlock()
	if ok {
		return program, nil
	}
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("CEL expression must return bool")
	}
	program, err := e.env.Program(ast, cel.CostLimit(10_000))
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.programs[expression] = program
	e.mu.Unlock()
	return program, nil
}

func safeActivation(in SafeInput) map[string]any {
	return map[string]any{
		"action":    map[string]any{"type": in.ActionType, "system": in.TargetSystem, "risk": normalizeRisk(in.RiskClass)},
		"resource":  map[string]any{"type": in.ResourceType, "reference": in.ResourceReference},
		"product":   map[string]any{"surface": in.ProductSurface},
		"requester": map[string]any{"type": in.RequesterType, "id": in.RequesterID, "membership_role": in.MembershipRole, "functional_roles": in.FunctionalRoles, "functional_scopes": in.FunctionalScopes},
		"authority": map[string]any{"hashes": in.AuthorityHashes},
		"time":      map[string]any{"utc_hour": in.Now.UTC().Hour(), "utc_day_of_week": int(in.Now.UTC().Weekday()), "unix": in.Now.UTC().Unix()},
	}
}

func precedenceDecision(versions []Version) string {
	decision := EffectAllow
	for _, version := range versions {
		if version.Effect == EffectDeny {
			return EffectDeny
		}
		if version.Effect == EffectRequireApproval {
			decision = EffectRequireApproval
		}
	}
	return decision
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	return "cel_evaluation_failed"
}

func (e *Evaluator) record(ctx context.Context, evaluation Evaluation) error {
	if e.recorder == nil {
		return nil
	}
	return e.recorder.RecordEvaluation(ctx, evaluation)
}

func (e *Evaluator) recordBestEffort(ctx context.Context, evaluation Evaluation) {
	_ = e.record(ctx, evaluation)
}

func NormalizeState(value string) string { return strings.ToLower(strings.TrimSpace(value)) }
