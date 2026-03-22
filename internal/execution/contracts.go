package execution

import (
	"context"
	"time"

	"stageflow/internal/domain"
)

// Dispatcher планирует асинхронное выполнение вне жизненного цикла HTTP-запроса.
type Dispatcher interface {
	Dispatch(ctx context.Context, job RunJob) error
}

// RunEventPublisher публикует структурированные события выполнения для live-отладки и истории запусков.
type RunEventPublisher interface {
	PublishRunEvent(ctx context.Context, event domain.RunEvent) error
}

// Engine выполняет конкретный запуск флоу, загружая текущее состояние запуска и определение флоу.
type Engine interface {
	ExecuteRun(ctx context.Context, runID domain.RunID) error
}

type HTTPExecutor interface {
	Execute(ctx context.Context, spec domain.RequestSpec, policy HTTPExecutionPolicy) (HTTPExecutionResult, error)
}

// TemplateResolver материализует HTTP-запрос из типизированной спецификации запроса и контекста выполнения.
type TemplateResolver interface {
	Resolve(ctx context.Context, spec domain.RequestSpec, data RenderData) (ResolvedRequest, error)
}

// Extractor извлекает типизированные результаты шага из HTTP-ответа.
type Extractor interface {
	Extract(ctx context.Context, spec domain.ExtractionSpec, response HTTPResponse) (map[string]any, error)
}

// Asserter проверяет ожидания к ответу и возвращает ошибки, относящиеся к проверкам.
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
