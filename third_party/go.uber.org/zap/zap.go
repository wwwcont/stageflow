package zap

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"

	"go.uber.org/zap/zapcore"
)

type Field struct {
	Key   string
	Value any
}

const (
	DebugLevel = zapcore.DebugLevel
	InfoLevel  = zapcore.InfoLevel
	WarnLevel  = zapcore.WarnLevel
	ErrorLevel = zapcore.ErrorLevel
)

type Option interface{ apply(*Logger) }
type optionFunc func(*Logger)

func (f optionFunc) apply(l *Logger)     { f(l) }
func AddCaller() Option                  { return optionFunc(func(*Logger) {}) }
func AddStacktrace(zapcore.Level) Option { return optionFunc(func(*Logger) {}) }

type AtomicLevel struct{ Level zapcore.Level }

func NewAtomicLevelAt(level zapcore.Level) AtomicLevel { return AtomicLevel{Level: level} }

type EncoderConfig struct{ TimeKey, MessageKey, CallerKey string }
type Config struct {
	Level            AtomicLevel
	OutputPaths      []string
	ErrorOutputPaths []string
	EncoderConfig    EncoderConfig
	InitialFields    map[string]interface{}
}

func NewProductionConfig() Config {
	return Config{Level: NewAtomicLevelAt(zapcore.InfoLevel), OutputPaths: []string{"stdout"}, ErrorOutputPaths: []string{"stderr"}, EncoderConfig: EncoderConfig{TimeKey: "ts", MessageKey: "msg", CallerKey: "caller"}, InitialFields: map[string]interface{}{}}
}

type Logger struct {
	mu     sync.Mutex
	level  zapcore.Level
	out    io.Writer
	fields []Field
}

func (c Config) Build(opts ...Option) (*Logger, error) {
	l := &Logger{level: c.Level.Level, out: os.Stdout}
	for k, v := range c.InitialFields {
		l.fields = append(l.fields, Field{Key: k, Value: v})
	}
	for _, opt := range opts {
		opt.apply(l)
	}
	return l, nil
}
func NewNop() *Logger         { return &Logger{level: zapcore.ErrorLevel + 100, out: io.Discard} }
func (l *Logger) Sync() error { return nil }
func (l *Logger) log(level string, lvl zapcore.Level, msg string, fields ...Field) {
	if l == nil || lvl < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	payload := map[string]any{"level": level, "ts": time.Now().UTC().Format(time.RFC3339Nano), "msg": msg}
	for _, f := range l.fields {
		payload[f.Key] = f.Value
	}
	for _, f := range fields {
		payload[f.Key] = f.Value
	}
	b, _ := json.Marshal(payload)
	_, _ = l.out.Write(append(b, '\n'))
}
func (l *Logger) Debug(msg string, fields ...Field) {
	l.log("debug", zapcore.DebugLevel, msg, fields...)
}
func (l *Logger) Info(msg string, fields ...Field) { l.log("info", zapcore.InfoLevel, msg, fields...) }
func (l *Logger) Warn(msg string, fields ...Field) { l.log("warn", zapcore.WarnLevel, msg, fields...) }
func (l *Logger) Error(msg string, fields ...Field) {
	l.log("error", zapcore.ErrorLevel, msg, fields...)
}
func String(k, v string) Field                 { return Field{Key: k, Value: v} }
func Int(k string, v int) Field                { return Field{Key: k, Value: v} }
func Duration(k string, v time.Duration) Field { return Field{Key: k, Value: v.String()} }
func Error(err error) Field {
	if err == nil {
		return Field{Key: "error", Value: nil}
	}
	return Field{Key: "error", Value: err.Error()}
}
func Any(k string, v any) Field           { return Field{Key: k, Value: v} }
func ByteString(k string, v []byte) Field { return Field{Key: k, Value: string(v)} }
func Bool(k string, v bool) Field         { return Field{Key: k, Value: v} }
