package telegram

import (
	"fmt"
	"io"

	"github.com/amarnathcjd/gogram/internal/utils"
)

// Logger interface allows users to provide custom logging implementations
type Logger interface {
	// Level control
	SetLevel(level LogLevel) Logger
	GetLevel() LogLevel

	// Output control
	SetOutput(w any) Logger
	SetFormatter(formatter LogFormatter) Logger

	// Context methods
	WithField(key string, value any) Logger
	WithFields(fields map[string]any) Logger
	WithError(err error) Logger
	WithPrefix(prefix string) Logger

	// Configuration
	SetColor(enabled bool) Logger
	Color() bool
	ShowCaller(enabled bool) Logger
	ShowFunction(enabled bool) Logger
	SetPrefix(prefix string) Logger
	Lev() LogLevel
	SetJSONMode(enabled bool) Logger

	// Logging methods
	Trace(msg any, args ...any)
	Debug(msg any, args ...any)
	Info(msg any, args ...any)
	Warn(msg any, args ...any)
	Warning(msg any, args ...any)
	Error(msg any, args ...any)
	Fatal(msg any, args ...any)
	Panic(msg any, args ...any)

	// Formatted logging
	Tracef(format string, args ...any)
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Warningf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Panicf(format string, args ...any)

	// Utility methods
	Flush() error
	Close() error
	Clone() Logger
	CloneInternal() *utils.Logger
}

type LogLevel int

const (
	TraceLevel LogLevel = iota
	DebugLevel
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
	PanicLevel
	NoLevel
)

const (
	LogTrace   = TraceLevel
	LogDebug   = DebugLevel
	LogInfo    = InfoLevel
	LogWarn    = WarnLevel
	LogError   = ErrorLevel
	LogFatal   = FatalLevel
	LogPanic   = PanicLevel
	LogNone    = NoLevel
	LogDisable = NoLevel
)

// LogFormatter defines the interface for custom log formatters
type LogFormatter interface {
	Format(entry *LogEntry) string
}

type LogEntry struct {
	Time       any            `json:"time"`
	Level      LogLevel       `json:"level"`
	Message    string         `json:"message"`
	Prefix     string         `json:"prefix,omitempty"`
	File       string         `json:"file,omitempty"`
	Line       int            `json:"line,omitempty"`
	Function   string         `json:"function,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
	Error      error          `json:"error,omitempty"`
	StackTrace string         `json:"stack_trace,omitempty"`
}

type Hook func(*LogEntry)

type LoggerConfig struct {
	Level           LogLevel
	Prefix          string
	Output          any // io.Writer
	Formatter       LogFormatter
	Color           bool
	ShowCaller      bool
	ShowFunction    bool
	TimestampFormat string
	BufferSize      int
	AsyncMode       bool
	AsyncQueueSize  int
	ErrorHandler    func(error)
	JSONOutput      bool // Enable JSON output mode
}

// LogOutputMode defines the output format
type LogOutputMode int

const (
	TextOutput LogOutputMode = iota
	JSONOutput
)

func NewDefaultLogger(prefix string) Logger {
	return &loggerAdapter{
		internal: utils.NewLogger(prefix),
	}
}

func NewLogger(level LogLevel, config ...LoggerConfig) Logger {
	internalConfig := &utils.LoggerConfig{
		Level: utils.LogLevel(level),
	}

	if len(config) > 0 {
		userConfig := config[0]
		internalConfig.Prefix = userConfig.Prefix
		if userConfig.Output != nil {
			if w, ok := userConfig.Output.(io.Writer); ok {
				internalConfig.Output = w
			}
		}
		internalConfig.Color = userConfig.Color
		internalConfig.ShowCaller = userConfig.ShowCaller
		internalConfig.ShowFunction = userConfig.ShowFunction
		internalConfig.TimestampFormat = userConfig.TimestampFormat
		internalConfig.BufferSize = userConfig.BufferSize
		internalConfig.AsyncMode = userConfig.AsyncMode
		internalConfig.AsyncQueueSize = userConfig.AsyncQueueSize
		internalConfig.ErrorHandler = userConfig.ErrorHandler
		if userConfig.JSONOutput {
			internalConfig.Formatter = &utils.JSONFormatter{}
		}
		if userConfig.Formatter != nil {
			// wrap the user formatter
			internalConfig.Formatter = &formatterAdapter{userFormatter: userConfig.Formatter}
		}
	}

	return &loggerAdapter{
		internal: utils.NewLoggerWithConfig(internalConfig),
	}
}

func NewLoggerWithConfig(config *LoggerConfig) Logger {
	internalConfig := &utils.LoggerConfig{
		Level:           utils.LogLevel(config.Level),
		Prefix:          config.Prefix,
		Color:           config.Color,
		ShowCaller:      config.ShowCaller,
		ShowFunction:    config.ShowFunction,
		TimestampFormat: config.TimestampFormat,
		BufferSize:      config.BufferSize,
		AsyncMode:       config.AsyncMode,
		AsyncQueueSize:  config.AsyncQueueSize,
		ErrorHandler:    config.ErrorHandler,
	}

	if config.Output != nil {
		if w, ok := config.Output.(io.Writer); ok {
			internalConfig.Output = w
		}
	}

	if config.JSONOutput {
		internalConfig.Formatter = &utils.JSONFormatter{}
	}

	if config.Formatter != nil {
		// Wrap the user formatter
		internalConfig.Formatter = &formatterAdapter{userFormatter: config.Formatter}
	}

	return &loggerAdapter{
		internal: utils.NewLoggerWithConfig(internalConfig),
	}
}

// loggerAdapter wraps internal logger to implement the public Logger interface
type loggerAdapter struct {
	internal *utils.Logger
}

func (l *loggerAdapter) SetLevel(level LogLevel) Logger {
	l.internal.SetLevel(utils.LogLevel(level))
	return l
}

func (l *loggerAdapter) GetLevel() LogLevel {
	return LogLevel(l.internal.GetLevel())
}

func (l *loggerAdapter) SetOutput(w any) Logger {
	if writer, ok := w.(io.Writer); ok {
		l.internal.SetOutput(writer)
	}
	return l
}

func (l *loggerAdapter) SetFormatter(formatter LogFormatter) Logger {
	if formatter != nil {
		l.internal.SetFormatter(&formatterAdapter{userFormatter: formatter})
	}
	return l
}

func (l *loggerAdapter) WithField(key string, value any) Logger {
	return &loggerAdapter{
		internal: l.internal.WithField(key, value),
	}
}

func (l *loggerAdapter) WithFields(fields map[string]any) Logger {
	return &loggerAdapter{
		internal: l.internal.WithFields(fields),
	}
}

func (l *loggerAdapter) WithError(err error) Logger {
	return &loggerAdapter{
		internal: l.internal.WithError(err),
	}
}

func (l *loggerAdapter) WithPrefix(prefix string) Logger {
	return &loggerAdapter{
		internal: l.internal.WithPrefix(prefix),
	}
}

func (l *loggerAdapter) SetColor(enabled bool) Logger {
	l.internal.SetColor(enabled)
	return l
}

func (l *loggerAdapter) Color() bool {
	return l.internal.Color()
}

func (l *loggerAdapter) ShowCaller(enabled bool) Logger {
	l.internal.ShowCaller(enabled)
	return l
}

func (l *loggerAdapter) ShowFunction(enabled bool) Logger {
	l.internal.ShowFunction(enabled)
	return l
}

func (l *loggerAdapter) SetPrefix(prefix string) Logger {
	l.internal.SetPrefix(prefix)
	return l
}

func (l *loggerAdapter) Lev() LogLevel {
	return LogLevel(l.internal.GetLevel())
}

func (l *loggerAdapter) SetJSONMode(enabled bool) Logger {
	if enabled {
		l.internal.SetFormatter(&utils.JSONFormatter{})
	} else {
		l.internal.SetFormatter(&utils.TextFormatter{})
	}
	return l
}

func (l *loggerAdapter) Trace(msg any, args ...any) {
	l.internal.Trace(toString(msg), args...)
}

func (l *loggerAdapter) Debug(msg any, args ...any) {
	l.internal.Debug(toString(msg), args...)
}

func (l *loggerAdapter) Info(msg any, args ...any) {
	l.internal.Info(toString(msg), args...)
}

func (l *loggerAdapter) Warn(msg any, args ...any) {
	l.internal.Warn(toString(msg), args...)
}

func (l *loggerAdapter) Warning(msg any, args ...any) {
	l.internal.Warning(toString(msg), args...)
}

func (l *loggerAdapter) Error(msg any, args ...any) {
	l.internal.Error(toString(msg), args...)
}

func (l *loggerAdapter) Fatal(msg any, args ...any) {
	l.internal.Fatal(toString(msg), args...)
}

func (l *loggerAdapter) Panic(msg any, args ...any) {
	l.internal.Panic(toString(msg), args...)
}

// toString converts any to string for logging
func toString(v any) string {
	if v == nil {
		return "<nil>"
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func (l *loggerAdapter) Tracef(format string, args ...any) {
	l.internal.Tracef(format, args...)
}

func (l *loggerAdapter) Debugf(format string, args ...any) {
	l.internal.Debugf(format, args...)
}

func (l *loggerAdapter) Infof(format string, args ...any) {
	l.internal.Infof(format, args...)
}

func (l *loggerAdapter) Warnf(format string, args ...any) {
	l.internal.Warnf(format, args...)
}

func (l *loggerAdapter) Warningf(format string, args ...any) {
	l.internal.Warningf(format, args...)
}

func (l *loggerAdapter) Errorf(format string, args ...any) {
	l.internal.Errorf(format, args...)
}

func (l *loggerAdapter) Fatalf(format string, args ...any) {
	l.internal.Fatalf(format, args...)
}

func (l *loggerAdapter) Panicf(format string, args ...any) {
	l.internal.Panicf(format, args...)
}

func (l *loggerAdapter) Flush() error {
	return l.internal.Flush()
}

func (l *loggerAdapter) Close() error {
	return l.internal.Close()
}

func (l *loggerAdapter) Clone() Logger {
	return &loggerAdapter{
		internal: l.internal.Clone(),
	}
}

func (l *loggerAdapter) CloneInternal() *utils.Logger {
	return l.internal.Clone()
}

// formatterAdapter adapts user-provided formatter to internal formatter
type formatterAdapter struct {
	userFormatter LogFormatter
}

func (f *formatterAdapter) Format(entry *utils.LogEntry) string {
	userEntry := &LogEntry{
		Time:       entry.Time,
		Level:      LogLevel(entry.Level),
		Message:    entry.Message,
		Prefix:     entry.Prefix,
		File:       entry.File,
		Line:       entry.Line,
		Function:   entry.Function,
		Fields:     entry.Fields,
		Error:      entry.Error,
		StackTrace: entry.StackTrace,
	}
	return f.userFormatter.Format(userEntry)
}
