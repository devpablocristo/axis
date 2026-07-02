package requests

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

type PolicyEvaluator struct {
	env    *cel.Env
	envErr error
	mu     sync.Mutex
	progs  map[string]cel.Program
}

func NewPolicyEvaluator() *PolicyEvaluator {
	env, err := cel.NewEnv(
		cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("time", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return &PolicyEvaluator{envErr: err, progs: make(map[string]cel.Program)}
	}
	return &PolicyEvaluator{env: env, progs: make(map[string]cel.Program)}
}

func (e *PolicyEvaluator) Matches(expression string, request map[string]any, now time.Time) (bool, error) {
	if strings.TrimSpace(expression) == "" {
		return true, nil
	}
	prog, err := e.program(expression)
	if err != nil {
		return false, err
	}
	m := map[string]any{
		"request": request,
		"time": map[string]any{
			"hour":        now.UTC().Hour(),
			"day_of_week": int(now.UTC().Weekday()),
		},
	}
	result, _, err := prog.Eval(m)
	if err != nil {
		return false, fmt.Errorf("eval policy: %w", err)
	}
	if result.Type() != types.BoolType {
		return false, fmt.Errorf("policy must return bool, got %s", result.Type())
	}
	b, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf("policy result is not bool")
	}
	return b, nil
}

func (e *PolicyEvaluator) Validate(expression string) error {
	_, err := e.program(expression)
	return err
}

func (e *PolicyEvaluator) program(expression string) (cel.Program, error) {
	if e.envErr != nil {
		return nil, e.envErr
	}
	e.mu.Lock()
	if p, ok := e.progs[expression]; ok {
		e.mu.Unlock()
		return p, nil
	}
	e.mu.Unlock()
	ast, issues := e.env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("expression must return bool")
	}
	prog, err := e.env.Program(ast)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.progs[expression] = prog
	e.mu.Unlock()
	return prog, nil
}
