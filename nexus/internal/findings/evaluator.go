package findings

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

type Evaluator struct {
	env    *cel.Env
	envErr error
	mu     sync.Mutex
	progs  map[string]cel.Program
}

func NewEvaluator() *Evaluator {
	env, err := cel.NewEnv(
		cel.Variable("facts", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("source", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("time", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return &Evaluator{envErr: err, progs: make(map[string]cel.Program)}
	}
	return &Evaluator{env: env, progs: make(map[string]cel.Program)}
}

func (e *Evaluator) Matches(expression string, facts map[string]any, source map[string]any, now time.Time) (bool, error) {
	if strings.TrimSpace(expression) == "" {
		return true, nil
	}
	prog, err := e.program(expression)
	if err != nil {
		return false, err
	}
	result, _, err := prog.Eval(map[string]any{
		"facts":  facts,
		"source": source,
		"time": map[string]any{
			"hour":        now.UTC().Hour(),
			"day_of_week": int(now.UTC().Weekday()),
		},
	})
	if err != nil {
		return false, fmt.Errorf("eval finding rule: %w", err)
	}
	if result.Type() != types.BoolType {
		return false, fmt.Errorf("finding rule must return bool, got %s", result.Type())
	}
	value, ok := result.Value().(bool)
	if !ok {
		return false, fmt.Errorf("finding rule result is not bool")
	}
	return value, nil
}

func (e *Evaluator) Validate(expression string) error {
	_, err := e.program(expression)
	return err
}

func (e *Evaluator) program(expression string) (cel.Program, error) {
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
