package execution

import (
	"context"
	"time"

	"stageflow/internal/domain"
)

// Dispatcher schedules asynchronous execution outside of the request path.
type Dispatcher interface {
	Dispatch(ctx context.Context, job RunJob) error
}

// RunEventPublisher emits structured execution events for live debugging and run history.
type RunEventPublisher interface {
	PublishRunEvent(ctx context.Context, event domain.RunEvent) error
}

// Engine executes a concrete flow run by loading the current run state and flow definition.
type Engine interface {
	ExecuteRun(ctx context.Context, runID domain.RunID) error
}

type HTTPExecutor interface {
	Execute(ctx context.Context, spec domain.RequestSpec, policy HTTPExecutionPolicy) (HTTPExecutionResult, error)
}

// TemplateResolver materializes an HTTP request from typed request spec and execution context.
type TemplateResolver interface {
	Resolve(ctx context.Context, spec domain.RequestSpec, data RenderData) (ResolvedRequest, error)
}

// Extractor derives typed step outputs from an HTTP response.
type Extractor interface {
	Extract(ctx context.Context, spec domain.ExtractionSpec, response HTTPResponse) (map[string]any, error)
}

// Asserter verifies response expectations and returns assertion-specific errors.
type Asserter interface {
	Assert(ctx context.Context, spec domain.AssertionSpec, response HTTPResponse) error
}

type RunJob struct {
	RunID          domain.RunID
	WorkspaceID    domain.WorkspaceID
	TargetType     domain.RunTargetType
	FlowID         domain.FlowID
	SavedRequestID domain.SavedRequestID
	Queue          string
}

type ResolvedRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    string
}

type HTTPResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}

type HTTPExecutionPolicy struct {
	AllowedHosts        []string
	MaxRequestBodyBytes int64
	DefaultTimeout      time.Duration
	SecretValues        map[string]string
}

type Sleeper interface {
	Sleep(ctx context.Context, delay time.Duration) error
}
