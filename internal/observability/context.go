package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"

	"stageflow/internal/domain"
)

type RunContext struct {
	WorkspaceID    domain.WorkspaceID
	RunID          domain.RunID
	FlowID         domain.FlowID
	SavedRequestID domain.SavedRequestID
	TargetType     domain.RunTargetType
}

type runContextKey struct{}

func WithRunContext(ctx context.Context, meta RunContext) context.Context {
	return context.WithValue(ctx, runContextKey{}, meta)
}

func RunContextFromContext(ctx context.Context) (RunContext, bool) {
	if ctx == nil {
		return RunContext{}, false
	}
	meta, ok := ctx.Value(runContextKey{}).(RunContext)
	return meta, ok
}

func RunContextAttributes(meta RunContext) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 5)
	if meta.WorkspaceID != "" {
		attrs = append(attrs, attribute.String("workspace.id", string(meta.WorkspaceID)))
	}
	if meta.RunID != "" {
		attrs = append(attrs, attribute.String("run.id", string(meta.RunID)))
	}
	if meta.TargetType != "" {
		attrs = append(attrs, attribute.String("run.target_type", string(meta.TargetType)))
	}
	if meta.FlowID != "" {
		attrs = append(attrs, attribute.String("flow.id", string(meta.FlowID)))
	}
	if meta.SavedRequestID != "" {
		attrs = append(attrs, attribute.String("saved_request.id", string(meta.SavedRequestID)))
	}
	return attrs
}

func RunContextLogFields(meta RunContext) []zap.Field {
	fields := make([]zap.Field, 0, 5)
	if meta.WorkspaceID != "" {
		fields = append(fields, zap.String("workspace_id", string(meta.WorkspaceID)))
	}
	if meta.RunID != "" {
		fields = append(fields, zap.String("run_id", string(meta.RunID)))
	}
	if meta.TargetType != "" {
		fields = append(fields, zap.String("target_type", string(meta.TargetType)))
	}
	if meta.FlowID != "" {
		fields = append(fields, zap.String("flow_id", string(meta.FlowID)))
	}
	if meta.SavedRequestID != "" {
		fields = append(fields, zap.String("saved_request_id", string(meta.SavedRequestID)))
	}
	return fields
}
