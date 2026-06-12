package runtime

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var (
	runtimeTracer      = otel.Tracer("axis.companion.runtime")
	runtimeMeter       = otel.Meter("axis.companion.runtime")
	runtimeRunsCounter metric.Int64Counter
	runtimeLLMCounter  metric.Int64Counter
	runtimeToolCounter metric.Int64Counter
)

func init() {
	runtimeRunsCounter, _ = runtimeMeter.Int64Counter("companion.runtime.runs")
	runtimeLLMCounter, _ = runtimeMeter.Int64Counter("companion.runtime.llm_calls")
	runtimeToolCounter, _ = runtimeMeter.Int64Counter("companion.runtime.tool_calls")
}

func startRunSpan(ctx context.Context, orgID, productSurface, agentID, model string) (context.Context, oteltrace.Span) {
	return runtimeTracer.Start(ctx, "companion.run", oteltrace.WithAttributes(
		attribute.String("org_id", orgID),
		attribute.String("product_surface", productSurface),
		attribute.String("agent_id", agentID),
		attribute.String("model", model),
	))
}

func recordRunMetrics(ctx context.Context, trace RunTrace, orgID string) {
	attrs := metric.WithAttributes(
		attribute.String("org_id", orgID),
		attribute.String("product_surface", trace.ProductSurface),
		attribute.String("agent_id", trace.IdentityChain.AgentID),
		attribute.String("model", trace.Model),
	)
	runtimeRunsCounter.Add(ctx, 1, attrs)
	runtimeLLMCounter.Add(ctx, int64(trace.Usage.LLMCalls), attrs)
	runtimeToolCounter.Add(ctx, int64(trace.Usage.ToolCalls), attrs)
}
