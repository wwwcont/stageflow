package config

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHTTPAddress     = ":8080"
	defaultShutdownTimeout = 10 * time.Second
	defaultLogLevel        = "info"
)

type Config struct {
	ServiceName   string
	Environment   string
	HTTP          HTTPConfig
	UI            UIConfig
	Log           LogConfig
	Redis         RedisConfig
	Worker        WorkerConfig
	Runtime       RuntimeConfig
	Observability ObservabilityConfig
	Metadata      MetadataConfig
}

type HTTPConfig struct {
	Address         string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type UIConfig struct {
	Enabled bool
	Prefix  string
}

type LogConfig struct {
	Level string
}

type RedisConfig struct {
	Addr      string
	Password  string
	DB        int
	KeyPrefix string
}

type WorkerConfig struct {
	ID                string
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	StaleRunTimeout   time.Duration
	RequeueDelay      time.Duration
	RecoveryBatchSize int
}

type RuntimeConfig struct {
	DefaultRunQueue string
	AllowedHosts    []string
}

type ObservabilityConfig struct {
	MetricsPath string
	Tracing     TracingConfig
}

type TracingConfig struct {
	Enabled     bool
	Exporter    string
	SampleRatio float64
}

type MetadataConfig struct {
	Version string
}

type EnvProvider interface {
	LookupEnv(key string) (string, bool)
}

type EnvSource struct{}

func (EnvSource) LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

func Load(_ context.Context, args []string, env EnvProvider) (Config, error) {
	if env == nil {
		env = EnvSource{}
	}

	parser := newEnvParser(env)

	cfg := Config{
		ServiceName: parser.string("STAGEFLOW_SERVICE_NAME", "stageflow"),
		Environment: parser.string("STAGEFLOW_ENV", "staging"),
		HTTP: HTTPConfig{
			Address:         parser.string("STAGEFLOW_HTTP_ADDR", defaultHTTPAddress),
			ReadTimeout:     parser.duration("STAGEFLOW_HTTP_READ_TIMEOUT", 5*time.Second),
			WriteTimeout:    parser.duration("STAGEFLOW_HTTP_WRITE_TIMEOUT", 15*time.Second),
			ShutdownTimeout: parser.duration("STAGEFLOW_HTTP_SHUTDOWN_TIMEOUT", defaultShutdownTimeout),
		},
		UI: UIConfig{
			Enabled: parser.bool("STAGEFLOW_UI_ENABLED", true),
			Prefix:  parser.string("STAGEFLOW_UI_PREFIX", "/ui"),
		},
		Log: LogConfig{Level: parser.string("STAGEFLOW_LOG_LEVEL", defaultLogLevel)},
		Redis: RedisConfig{
			Addr:      parser.string("STAGEFLOW_REDIS_ADDR", "127.0.0.1:6379"),
			Password:  parser.string("STAGEFLOW_REDIS_PASSWORD", ""),
			DB:        parser.int("STAGEFLOW_REDIS_DB", 0),
			KeyPrefix: parser.string("STAGEFLOW_REDIS_PREFIX", "stageflow"),
		},
		Worker: WorkerConfig{
			ID:                parser.string("STAGEFLOW_WORKER_ID", fmt.Sprintf("%s-worker", parser.string("STAGEFLOW_SERVICE_NAME", "stageflow"))),
			PollInterval:      parser.duration("STAGEFLOW_WORKER_POLL_INTERVAL", time.Second),
			LeaseDuration:     parser.duration("STAGEFLOW_WORKER_LEASE_DURATION", 30*time.Second),
			HeartbeatInterval: parser.duration("STAGEFLOW_WORKER_HEARTBEAT_INTERVAL", 10*time.Second),
			StaleRunTimeout:   parser.duration("STAGEFLOW_WORKER_STALE_RUN_TIMEOUT", 45*time.Second),
			RequeueDelay:      parser.duration("STAGEFLOW_WORKER_REQUEUE_DELAY", 2*time.Second),
			RecoveryBatchSize: parser.int("STAGEFLOW_WORKER_RECOVERY_BATCH_SIZE", 10),
		},
		Runtime: RuntimeConfig{
			DefaultRunQueue: parser.string("STAGEFLOW_DEFAULT_RUN_QUEUE", "default"),
			AllowedHosts:    parser.csv("STAGEFLOW_ALLOWED_HOSTS", []string{"example.internal", "host.docker.internal", "localhost"}),
		},
		Observability: ObservabilityConfig{
			MetricsPath: parser.string("STAGEFLOW_METRICS_PATH", "/metrics"),
			Tracing: TracingConfig{
				Enabled:     parser.bool("STAGEFLOW_TRACING_ENABLED", true),
				Exporter:    parser.string("STAGEFLOW_TRACING_EXPORTER", "stdout"),
				SampleRatio: parser.float("STAGEFLOW_TRACING_SAMPLE_RATIO", 1),
			},
		},
		Metadata: MetadataConfig{Version: parser.string("STAGEFLOW_VERSION", "dev")},
	}

	fs := flag.NewFlagSet("stageflow", flag.ContinueOnError)
	fs.StringVar(&cfg.ServiceName, "service-name", cfg.ServiceName, "logical service name")
	fs.StringVar(&cfg.Environment, "env", cfg.Environment, "deployment environment")
	fs.StringVar(&cfg.HTTP.Address, "http-addr", cfg.HTTP.Address, "HTTP listen address")
	fs.DurationVar(&cfg.HTTP.ReadTimeout, "http-read-timeout", cfg.HTTP.ReadTimeout, "HTTP read timeout")
	fs.DurationVar(&cfg.HTTP.WriteTimeout, "http-write-timeout", cfg.HTTP.WriteTimeout, "HTTP write timeout")
	fs.DurationVar(&cfg.HTTP.ShutdownTimeout, "http-shutdown-timeout", cfg.HTTP.ShutdownTimeout, "graceful shutdown timeout")
	fs.BoolVar(&cfg.UI.Enabled, "ui-enabled", cfg.UI.Enabled, "enable built-in server-rendered UI")
	fs.StringVar(&cfg.UI.Prefix, "ui-prefix", cfg.UI.Prefix, "path prefix used to serve the built-in UI")
	fs.StringVar(&cfg.Log.Level, "log-level", cfg.Log.Level, "log level (debug|info|warn|error)")
	fs.StringVar(&cfg.Redis.Addr, "redis-addr", cfg.Redis.Addr, "Redis server address")
	fs.StringVar(&cfg.Redis.Password, "redis-password", cfg.Redis.Password, "Redis password")
	fs.IntVar(&cfg.Redis.DB, "redis-db", cfg.Redis.DB, "Redis logical DB index")
	fs.StringVar(&cfg.Redis.KeyPrefix, "redis-prefix", cfg.Redis.KeyPrefix, "Redis key prefix for StageFlow")
	fs.StringVar(&cfg.Worker.ID, "worker-id", cfg.Worker.ID, "worker identifier")
	fs.DurationVar(&cfg.Worker.PollInterval, "worker-poll-interval", cfg.Worker.PollInterval, "worker poll interval")
	fs.DurationVar(&cfg.Worker.LeaseDuration, "worker-lease-duration", cfg.Worker.LeaseDuration, "worker lease duration")
	fs.DurationVar(&cfg.Worker.HeartbeatInterval, "worker-heartbeat-interval", cfg.Worker.HeartbeatInterval, "worker heartbeat interval")
	fs.DurationVar(&cfg.Worker.StaleRunTimeout, "worker-stale-run-timeout", cfg.Worker.StaleRunTimeout, "how old a running run heartbeat must be before takeover is allowed")
	fs.DurationVar(&cfg.Worker.RequeueDelay, "worker-requeue-delay", cfg.Worker.RequeueDelay, "delay before releasing a conflicting job back to the queue")
	fs.IntVar(&cfg.Worker.RecoveryBatchSize, "worker-recovery-batch-size", cfg.Worker.RecoveryBatchSize, "max number of expired leases to requeue per recovery cycle")
	fs.StringVar(&cfg.Runtime.DefaultRunQueue, "default-run-queue", cfg.Runtime.DefaultRunQueue, "default queue for asynchronous run execution")
	fs.Func("allowed-host", "allowed outbound HTTP host; repeat flag to add more than one host", func(value string) error {
		cfg.Runtime.AllowedHosts = append(cfg.Runtime.AllowedHosts, strings.TrimSpace(value))
		return nil
	})
	fs.StringVar(&cfg.Observability.MetricsPath, "metrics-path", cfg.Observability.MetricsPath, "HTTP path used to expose Prometheus metrics")
	fs.BoolVar(&cfg.Observability.Tracing.Enabled, "tracing-enabled", cfg.Observability.Tracing.Enabled, "enable OpenTelemetry tracing")
	fs.StringVar(&cfg.Observability.Tracing.Exporter, "tracing-exporter", cfg.Observability.Tracing.Exporter, "OpenTelemetry tracing exporter (stdout|none)")
	fs.Float64Var(&cfg.Observability.Tracing.SampleRatio, "tracing-sample-ratio", cfg.Observability.Tracing.SampleRatio, "OpenTelemetry trace sampling ratio from 0.0 to 1.0")
	fs.StringVar(&cfg.Metadata.Version, "version", cfg.Metadata.Version, "service version identifier")

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	if err := parser.err(); err != nil {
		return Config{}, err
	}

	cfg.normalize()

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c *Config) normalize() {
	c.ServiceName = strings.TrimSpace(c.ServiceName)
	c.Environment = strings.TrimSpace(c.Environment)
	c.HTTP.Address = strings.TrimSpace(c.HTTP.Address)
	c.Log.Level = strings.ToLower(strings.TrimSpace(c.Log.Level))
	c.UI.Prefix = strings.TrimSpace(c.UI.Prefix)
	c.Redis.Addr = strings.TrimSpace(c.Redis.Addr)
	c.Redis.KeyPrefix = strings.TrimSpace(c.Redis.KeyPrefix)
	c.Worker.ID = strings.TrimSpace(c.Worker.ID)
	c.Runtime.DefaultRunQueue = strings.TrimSpace(c.Runtime.DefaultRunQueue)
	c.Runtime.AllowedHosts = normalizeHosts(c.Runtime.AllowedHosts)
	c.Observability.MetricsPath = strings.TrimSpace(c.Observability.MetricsPath)
	c.Observability.Tracing.Exporter = strings.ToLower(strings.TrimSpace(c.Observability.Tracing.Exporter))
	c.Metadata.Version = strings.TrimSpace(c.Metadata.Version)
}

func (c Config) Validate() error {
	var errs []error
	if c.ServiceName == "" {
		errs = append(errs, errors.New("service name is required"))
	}
	if c.HTTP.Address == "" {
		errs = append(errs, errors.New("http address is required"))
	}
	if c.HTTP.ReadTimeout <= 0 {
		errs = append(errs, errors.New("http read timeout must be positive"))
	}
	if c.HTTP.WriteTimeout <= 0 {
		errs = append(errs, errors.New("http write timeout must be positive"))
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("http shutdown timeout must be positive"))
	}
	if strings.TrimSpace(c.UI.Prefix) == "" || !strings.HasPrefix(c.UI.Prefix, "/") {
		errs = append(errs, errors.New("ui prefix must start with '/'"))
	}
	if c.Runtime.DefaultRunQueue == "" {
		errs = append(errs, errors.New("default run queue is required"))
	}
	if len(c.Runtime.AllowedHosts) == 0 {
		errs = append(errs, errors.New("at least one allowed outbound HTTP host is required"))
	}
	if !isAllowedLogLevel(c.Log.Level) {
		errs = append(errs, fmt.Errorf("unsupported log level %q", c.Log.Level))
	}
	if strings.TrimSpace(c.Observability.MetricsPath) == "" || !strings.HasPrefix(c.Observability.MetricsPath, "/") {
		errs = append(errs, errors.New("metrics path must start with '/'"))
	}
	if !isAllowedTracingExporter(c.Observability.Tracing.Exporter) {
		errs = append(errs, fmt.Errorf("unsupported tracing exporter %q", c.Observability.Tracing.Exporter))
	}
	if c.Observability.Tracing.SampleRatio < 0 || c.Observability.Tracing.SampleRatio > 1 {
		errs = append(errs, errors.New("tracing sample ratio must be between 0 and 1"))
	}
	if c.Redis.Addr == "" {
		errs = append(errs, errors.New("redis address is required"))
	}
	if c.Redis.KeyPrefix == "" {
		errs = append(errs, errors.New("redis key prefix is required"))
	}
	if c.Worker.ID == "" {
		errs = append(errs, errors.New("worker id is required"))
	}
	if c.Worker.PollInterval <= 0 {
		errs = append(errs, errors.New("worker poll interval must be positive"))
	}
	if c.Worker.LeaseDuration <= 0 {
		errs = append(errs, errors.New("worker lease duration must be positive"))
	}
	if c.Worker.HeartbeatInterval <= 0 || c.Worker.HeartbeatInterval >= c.Worker.LeaseDuration {
		errs = append(errs, errors.New("worker heartbeat interval must be positive and less than lease duration"))
	}
	if c.Worker.StaleRunTimeout <= 0 {
		errs = append(errs, errors.New("worker stale run timeout must be positive"))
	}
	if c.Worker.RequeueDelay < 0 {
		errs = append(errs, errors.New("worker requeue delay must be non-negative"))
	}
	if c.Worker.RecoveryBatchSize <= 0 {
		errs = append(errs, errors.New("worker recovery batch size must be positive"))
	}
	return errors.Join(errs...)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func normalizeHosts(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(hosts))
	result := make([]string, 0, len(hosts))
	for _, host := range hosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

type envParser struct {
	env  EnvProvider
	errs []error
}

func newEnvParser(env EnvProvider) *envParser {
	return &envParser{env: env}
}

func (p *envParser) string(key, fallback string) string {
	if value, ok := p.env.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func (p *envParser) duration(key string, fallback time.Duration) time.Duration {
	raw, ok := p.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s must be a valid duration: %w", key, err))
		return fallback
	}
	return parsed
}

func (p *envParser) bool(key string, fallback bool) bool {
	raw, ok := p.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s must be a valid boolean: %w", key, err))
		return fallback
	}
	return parsed
}

func (p *envParser) int(key string, fallback int) int {
	raw, ok := p.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s must be a valid integer: %w", key, err))
		return fallback
	}
	return parsed
}

func (p *envParser) float(key string, fallback float64) float64 {
	raw, ok := p.lookup(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		p.errs = append(p.errs, fmt.Errorf("%s must be a valid float: %w", key, err))
		return fallback
	}
	return parsed
}

func (p *envParser) csv(key string, fallback []string) []string {
	if raw, ok := p.lookup(key); ok {
		return splitCSV(raw)
	}
	result := make([]string, 0, len(fallback))
	for _, item := range fallback {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func (p *envParser) lookup(key string) (string, bool) {
	value, ok := p.env.LookupEnv(key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func (p *envParser) err() error {
	return errors.Join(p.errs...)
}

func isAllowedLogLevel(level string) bool {
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

func isAllowedTracingExporter(exporter string) bool {
	switch strings.ToLower(strings.TrimSpace(exporter)) {
	case "stdout", "none":
		return true
	default:
		return false
	}
}
