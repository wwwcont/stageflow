package observability

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"stageflow/internal/config"
)

func newZapLogger(cfg config.Config) (*zap.Logger, error) {
	level, err := parseLevel(cfg.Log.Level)
	if err != nil {
		return nil, err
	}

	zapCfg := zap.NewProductionConfig()
	zapCfg.Level = zap.NewAtomicLevelAt(level)
	zapCfg.OutputPaths = []string{"stdout"}
	zapCfg.ErrorOutputPaths = []string{"stderr"}
	zapCfg.EncoderConfig.TimeKey = "ts"
	zapCfg.EncoderConfig.MessageKey = "msg"
	zapCfg.EncoderConfig.CallerKey = "caller"
	zapCfg.InitialFields = map[string]interface{}{
		"service": cfg.ServiceName,
		"env":     cfg.Environment,
		"version": cfg.Metadata.Version,
	}

	return zapCfg.Build(zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

func parseLevel(raw string) (zapcore.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return zap.DebugLevel, nil
	case "info":
		return zap.InfoLevel, nil
	case "warn":
		return zap.WarnLevel, nil
	case "error":
		return zap.ErrorLevel, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", raw)
	}
}
