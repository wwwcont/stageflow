package http

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type contextKey string

const requestIDKey contextKey = "request_id"

type MetricsHook interface {
	ObserveRequest(ctx context.Context, method, route string, status int, duration time.Duration)
}

type noopMetrics struct{}

func (noopMetrics) ObserveRequest(context.Context, string, string, int, time.Duration) {}

type middleware struct {
	logger      *zap.Logger
	metrics     MetricsHook
	requestSeed atomic.Uint64
	tracer      trace.Tracer
}

func newMiddleware(logger *zap.Logger, metrics MetricsHook) *middleware {
	if logger == nil {
		logger = zap.NewNop()
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &middleware{logger: logger, metrics: metrics, tracer: otel.Tracer("stageflow/http")}
}

func (m *middleware) chain(next http.Handler) http.Handler {
	return m.requestID(m.recovery(m.logging(next)))
}

func (m *middleware) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = fmt.Sprintf("req-%d", m.requestSeed.Add(1))
		}
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *middleware) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				m.logger.Error("http panic recovered",
					zap.Any("panic", recovered),
					zap.String("request_id", requestIDFromContext(r.Context())),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.ByteString("stack", debug.Stack()),
				)
				writeJSON(w, http.StatusInternalServerError, errorResponse{
					Error:     apiError{Code: "internal_error", Message: "internal server error"},
					RequestID: requestIDFromContext(r.Context()),
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (m *middleware) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		route := r.Pattern
		if route == "" {
			route = r.URL.Path
		}

		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := m.tracer.Start(ctx, route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.route", route),
			attribute.String("http.target", r.URL.Path),
			attribute.String("request.id", requestIDFromContext(r.Context())),
		))
		defer span.End()

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r.WithContext(ctx))
		duration := time.Since(startedAt)

		span.SetAttributes(
			attribute.Int("http.status_code", recorder.status),
			attribute.String("http.duration", duration.String()),
		)
		if recorder.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(recorder.status))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		m.metrics.ObserveRequest(ctx, r.Method, route, recorder.status, duration)
		m.logger.Info("http request completed",
			zap.String("request_id", requestIDFromContext(r.Context())),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("route", route),
			zap.Int("status", recorder.status),
			zap.Duration("duration", duration),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey).(string)
	return requestID
}
