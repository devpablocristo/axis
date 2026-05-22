package learning

import (
	"context"

	requestdomain "github.com/devpablocristo/nexus/internal/requests/usecases/domain"
)

// RequestLister es el port para obtener requests históticas para análisis.
// El learning analyzer corre cross-org (admin scope), de ahí que el único
// call site invoque List con allowAll=true.
type RequestLister interface {
	List(ctx context.Context, status, actionType string, limit int, orgID *string, allowAll bool) ([]requestdomain.Request, error)
}

// InMemoryPatternAnalyzer analiza patrones sobre requests en memoria.
type InMemoryPatternAnalyzer struct {
	requestLister RequestLister
}

func NewInMemoryPatternAnalyzer(rl RequestLister) *InMemoryPatternAnalyzer {
	return &InMemoryPatternAnalyzer{requestLister: rl}
}

func (a *InMemoryPatternAnalyzer) Analyze(ctx context.Context, timeWindowDays, minSampleSize int, minApprovalRate float64) ([]Pattern, error) {
	// Obtener todas las requests que pasaron por approval
	// Learning analyzer es admin/cross-org: ver todas las requests para detectar patrones.
	allRequests, err := a.requestLister.List(ctx, "", "", 10000, nil, true)
	if err != nil {
		return nil, err
	}

	// Agrupar por action_type y contar decisiones
	type stats struct {
		total    int
		approved int
		denied   int
	}
	byAction := make(map[string]*stats)
	for _, r := range allRequests {
		if r.Decision != requestdomain.DecisionRequireApproval {
			continue
		}
		s, ok := byAction[r.ActionType]
		if !ok {
			s = &stats{}
			byAction[r.ActionType] = s
		}
		s.total++
		switch r.Status {
		case requestdomain.StatusApproved, requestdomain.StatusExecuted:
			s.approved++
		case requestdomain.StatusRejected:
			s.denied++
		}
	}

	var patterns []Pattern
	for actionType, s := range byAction {
		if s.total < minSampleSize {
			continue
		}
		rate := float64(s.approved) / float64(s.total)
		if rate >= minApprovalRate {
			patterns = append(patterns, Pattern{
				ActionType:   actionType,
				Total:        s.total,
				Approved:     s.approved,
				ApprovalRate: rate,
				TimeWindow:   "all",
			})
		}
	}
	return patterns, nil
}
