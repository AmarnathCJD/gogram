package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

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

func (l LogLevel) String() string {
	switch l {
	case TraceLevel:
		return "TRACE"
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case FatalLevel:
		return "FATAL"
	case PanicLevel:
		return "PANIC"
	case NoLevel:
		return "NONE"
	default:
		return "UNKNOWN"
	}
}

var (
	colorReset       = "\033[0m"
	colorBold        = "\033[1m"
	colorDim         = "\033[2m"
	colorRed         = "\033[31m"
	colorGreen       = "\033[32m"
	colorYellow      = "\033[33m"
	colorBlue        = "\033[34m"
	colorMagenta     = "\033[35m"
	colorCyan        = "\033[36m"
	colorWhite       = "\033[37m"
	colorBrightBlack = "\033[90m"
	colorBrightRed   = "\033[91m"
)

type LogFormatter interface {
	Format(entry *LogEntry) string
}

type LogEntry struct {
	Time       time.Time      `json:"time"`
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

type Logger struct {
	mu              sync.RWMutex
	level           LogLevel
	prefix          string
	output          io.Writer
	writer          *bufio.Writer
	formatter       LogFormatter
	hooks           []Hook
	fields          map[string]any
	color           bool
	showCaller      bool
	showFunction    bool
	timestampFormat string
	bufferSize      int
	asyncMode       bool
	logChan         chan *LogEntry
	wg              sync.WaitGroup
	closed          bool
	errorHandler    func(error)
	rotationEnabled bool
	maxFileSize     int64
	currentFileSize int64
	logFilePath     string
}

type LoggerConfig struct {
	Level           LogLevel
	Prefix          string
	Output          io.Writer
	Formatter       LogFormatter
	Color           bool
	ShowCaller      bool
	ShowFunction    bool
	TimestampFormat string
	BufferSize      int
	AsyncMode       bool
	AsyncQueueSize  int
	ErrorHandler    func(error)
}

func DefaultConfig() *LoggerConfig {
	return &LoggerConfig{
		Level:           InfoLevel,
		Output:          os.Stdout,
		Formatter:       &TextFormatter{},
		Color:           false,
		ShowCaller:      true,
		TimestampFormat: "2006-01-02 15:04:05.000",
		BufferSize:      4096,
		AsyncQueueSize:  1000,
	}
}

func NewLogger(prefix string) *Logger {
	config := DefaultConfig()
	config.Prefix = prefix
	return NewLoggerWithConfig(config)
}

func NewLoggerWithConfig(config *LoggerConfig) *Logger {
	if config == nil {
		config = DefaultConfig()
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.Formatter == nil {
		config.Formatter = &TextFormatter{NoColor: !config.Color}
	}
	if config.TimestampFormat == "" {
		config.TimestampFormat = "2006-01-02 15:04:05.000"
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 4096
	}
	if config.AsyncQueueSize <= 0 {
		config.AsyncQueueSize = 1000
	}

	logger := &Logger{
		level:           config.Level,
		prefix:          config.Prefix,
		output:          config.Output,
		writer:          bufio.NewWriterSize(config.Output, config.BufferSize),
		formatter:       config.Formatter,
		color:           config.Color,
		showCaller:      config.ShowCaller,
		showFunction:    config.ShowFunction,
		timestampFormat: config.TimestampFormat,
		bufferSize:      config.BufferSize,
		asyncMode:       config.AsyncMode,
		fields:          make(map[string]any),
		errorHandler:    config.ErrorHandler,
	}

	if config.AsyncMode {
		logger.logChan = make(chan *LogEntry, config.AsyncQueueSize)
		logger.wg.Add(1)
		go logger.processAsync()
	}

	return logger
}

func (l *Logger) Clone() *Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()

	clone := &Logger{
		level:           l.level,
		prefix:          l.prefix,
		output:          l.output,
		writer:          bufio.NewWriterSize(l.output, l.bufferSize),
		formatter:       l.formatter,
		color:           l.color,
		showCaller:      l.showCaller,
		showFunction:    l.showFunction,
		timestampFormat: l.timestampFormat,
		bufferSize:      l.bufferSize,
		asyncMode:       l.asyncMode,
		fields:          make(map[string]any),
		errorHandler:    l.errorHandler,
	}

	maps.Copy(clone.fields, l.fields)

	return clone
}

func (l *Logger) WithPrefix(prefix string) *Logger {
	clone := l.Clone()
	clone.prefix = prefix
	return clone
}

func (l *Logger) WithField(key string, value any) *Logger {
	clone := l.Clone()
	clone.fields[key] = value
	return clone
}

func (l *Logger) WithFields(fields map[string]any) *Logger {
	clone := l.Clone()
	maps.Copy(clone.fields, fields)
	return clone
}

func (l *Logger) WithError(err error) *Logger {
	clone := l.Clone()
	clone.fields["error"] = err
	return clone
}

func (l *Logger) SetLevel(level LogLevel) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
	return l
}

func (l *Logger) GetLevel() LogLevel {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

func (l *Logger) Lev() LogLevel {
	return l.GetLevel()
}

func (l *Logger) GetPrefix() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.prefix
}

func (l *Logger) SetPrefix(prefix string) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prefix = prefix
	return l
}

func (l *Logger) Color() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.color
}

func (l *Logger) SetOutput(w io.Writer) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Flush()
	l.output = w
	l.writer = bufio.NewWriterSize(w, l.bufferSize)
	return l
}

func (l *Logger) SetFormatter(formatter LogFormatter) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.formatter = formatter
	return l
}

func (l *Logger) AddHook(hook Hook) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hooks = append(l.hooks, hook)
	return l
}

func (l *Logger) SetColor(enabled bool) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.color = enabled
	// Sync with TextFormatter if present
	if tf, ok := l.formatter.(*TextFormatter); ok {
		tf.NoColor = !enabled
	}
	return l
}

func (l *Logger) ShowCaller(enabled bool) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.showCaller = enabled
	return l
}

func (l *Logger) ShowFunction(enabled bool) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.showFunction = enabled
	return l
}

func (l *Logger) SetTimestampFormat(format string) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.timestampFormat = format
	return l
}

func (l *Logger) EnableRotation(maxFileSize int64, logFilePath string) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rotationEnabled = true
	l.maxFileSize = maxFileSize
	l.logFilePath = logFilePath
	return l
}

func (l *Logger) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writer.Flush()
}

func (l *Logger) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true

	if l.asyncMode && l.logChan != nil {
		close(l.logChan)
		l.mu.Unlock()
		l.wg.Wait()
		l.mu.Lock()
	}

	err := l.writer.Flush()
	l.mu.Unlock()
	return err
}

// processAsync handles asynchronous log processing
func (l *Logger) processAsync() {
	defer l.wg.Done()
	for entry := range l.logChan {
		l.writeEntry(entry)
	}
}

// core fn to log messages
func (l *Logger) log(level LogLevel, msg string, args ...any) {
	l.mu.RLock()
	if level < l.level || l.closed {
		l.mu.RUnlock()
		return
	}
	l.mu.RUnlock()

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	var file string
	var line int
	var function string

	if l.showCaller || l.showFunction {
		pc, f, lineNum, ok := runtime.Caller(2)
		if ok {
			file = filepath.Base(f)
			line = lineNum
			if l.showFunction {
				fn := runtime.FuncForPC(pc)
				if fn != nil {
					function = filepath.Base(fn.Name())
				}
			}
		}
	}

	entry := &LogEntry{
		Time:     time.Now(),
		Level:    level,
		Message:  msg,
		Prefix:   l.prefix,
		File:     file,
		Line:     line,
		Function: function,
		Fields:   make(map[string]any),
	}

	l.mu.RLock()
	maps.Copy(entry.Fields, l.fields)
	if err, ok := entry.Fields["error"].(error); ok {
		entry.Error = err
		delete(entry.Fields, "error")
	}
	l.mu.RUnlock()

	l.mu.RLock()
	for _, hook := range l.hooks {
		hook(entry)
	}
	l.mu.RUnlock()

	if l.asyncMode {
		select {
		case l.logChan <- entry:
		default:
			l.writeEntry(entry)
		}
	} else {
		l.writeEntry(entry)
	}
}

func (l *Logger) writeEntry(entry *LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	formatted := l.formatter.Format(entry)

	if l.rotationEnabled {
		l.currentFileSize += int64(len(formatted))
		if l.currentFileSize >= l.maxFileSize {
			l.rotate()
		}
	}

	_, err := l.writer.WriteString(formatted)
	if err != nil && l.errorHandler != nil {
		l.errorHandler(err)
	}
	l.writer.Flush()
}

func (l *Logger) rotate() {
	if l.logFilePath == "" {
		return
	}

	l.writer.Flush()

	timestamp := time.Now().Format("20060102-150405")
	newName := fmt.Sprintf("%s.%s", l.logFilePath, timestamp)
	os.Rename(l.logFilePath, newName)

	f, err := os.OpenFile(l.logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		if l.errorHandler != nil {
			l.errorHandler(err)
		}
		return
	}

	l.output = f
	l.writer = bufio.NewWriterSize(f, l.bufferSize)
	l.currentFileSize = 0
}

func (l *Logger) Trace(msg string, args ...any)   { l.log(TraceLevel, msg, args...) }
func (l *Logger) Debug(msg string, args ...any)   { l.log(DebugLevel, msg, args...) }
func (l *Logger) Info(msg string, args ...any)    { l.log(InfoLevel, msg, args...) }
func (l *Logger) Warn(msg string, args ...any)    { l.log(WarnLevel, msg, args...) }
func (l *Logger) Warning(msg string, args ...any) { l.log(WarnLevel, msg, args...) }
func (l *Logger) Error(msg string, args ...any)   { l.log(ErrorLevel, msg, args...) }

func (l *Logger) TraceErr(err error) { l.WithError(err).Trace(err.Error()) }
func (l *Logger) DebugErr(err error) { l.WithError(err).Debug(err.Error()) }
func (l *Logger) WarnErr(err error)  { l.WithError(err).Warn(err.Error()) }
func (l *Logger) ErrorErr(err error) { l.WithError(err).Error(err.Error()) }

func (l *Logger) Fatal(msg string, args ...any) {
	l.log(FatalLevel, msg, args...)
	l.Close()
	os.Exit(1)
}

func (l *Logger) Panic(msg string, args ...any) {
	// capture stack trace
	stack := make([]byte, 8192)
	n := runtime.Stack(stack, false)
	stackTrace := string(stack[:n])

	entry := &LogEntry{
		Time:       time.Now(),
		Level:      PanicLevel,
		Message:    fmt.Sprintf(msg, args...),
		Prefix:     l.prefix,
		StackTrace: stackTrace,
		Fields:     make(map[string]any),
	}

	l.mu.RLock()
	maps.Copy(entry.Fields, l.fields)
	l.mu.RUnlock()

	l.writeEntry(entry)
	l.Close()
	panic(entry.Message)
}

func (l *Logger) Tracef(format string, args ...any)   { l.Trace(format, args...) }
func (l *Logger) Debugf(format string, args ...any)   { l.Debug(format, args...) }
func (l *Logger) Infof(format string, args ...any)    { l.Info(format, args...) }
func (l *Logger) Warnf(format string, args ...any)    { l.Warn(format, args...) }
func (l *Logger) Warningf(format string, args ...any) { l.Warning(format, args...) }
func (l *Logger) Errorf(format string, args ...any)   { l.Error(format, args...) }
func (l *Logger) Fatalf(format string, args ...any)   { l.Fatal(format, args...) }
func (l *Logger) Panicf(format string, args ...any)   { l.Panic(format, args...) }

// TextFormatter formats logs as human-readable text
type TextFormatter struct {
	NoColor         bool
	ShowTimestamp   bool
	ShowLevel       bool
	ShowCaller      bool
	ShowFunction    bool
	TimestampFormat string
	FullTimestamp   bool
	PadLevelText    bool
}

func (f *TextFormatter) Format(entry *LogEntry) string {
	var b strings.Builder

	timestamp := entry.Time.Format("15:04:05")
	if !f.NoColor {
		b.WriteString(colorDim)
	}
	b.WriteString(timestamp)
	if !f.NoColor {
		b.WriteString(colorReset)
	}

	levelText := fmt.Sprintf(" %-5s", entry.Level.String())
	levelColor := f.getLevelColor(entry.Level)
	if !f.NoColor && levelColor != "" {
		b.WriteString(levelColor)
		b.WriteString(colorBold)
	}
	b.WriteString(levelText)
	if !f.NoColor && levelColor != "" {
		b.WriteString(colorReset)
	}

	if entry.Prefix != "" {
		b.WriteString(" ")
		if !f.NoColor {
			b.WriteString(colorDim)
		}
		b.WriteString("[")
		if !f.NoColor {
			b.WriteString(colorReset)
			b.WriteString(colorBrightBlack)
		}
		b.WriteString(entry.Prefix)
		if !f.NoColor {
			b.WriteString(colorReset)
			b.WriteString(colorDim)
		}
		b.WriteString("]")
		if !f.NoColor {
			b.WriteString(colorReset)
		}
	}

	if entry.File != "" && entry.Line > 0 {
		if !f.NoColor {
			b.WriteString(colorDim)
		}
		b.WriteString(" ")
		b.WriteString(entry.File)
		b.WriteString(":")
		b.WriteString(fmt.Sprint(entry.Line))
		if !f.NoColor {
			b.WriteString(colorReset)
		}
	}

	b.WriteString(" ")
	if !f.NoColor {
		msgColor := f.getMessageColor(entry.Level)
		if msgColor != "" {
			b.WriteString(msgColor)
		}
	}
	b.WriteString(entry.Message)
	if !f.NoColor {
		b.WriteString(colorReset)
	}

	if len(entry.Fields) > 0 {
		if !f.NoColor {
			b.WriteString(colorDim)
		}
		first := true
		for k, v := range entry.Fields {
			if first {
				b.WriteString(" ")
				first = false
			} else {
				b.WriteString(", ")
			}
			b.WriteString(k)
			b.WriteString("=")
			if !f.NoColor {
				b.WriteString(colorReset)
				b.WriteString(colorCyan)
			}
			b.WriteString(fmt.Sprintf("%v", v))
			if !f.NoColor {
				b.WriteString(colorDim)
			}
		}
		if !f.NoColor {
			b.WriteString(colorReset)
		}
	}

	if entry.Error != nil {
		if !f.NoColor {
			b.WriteString(" ")
			b.WriteString(colorBrightRed)
		} else {
			b.WriteString(" ")
		}
		b.WriteString("error=")
		b.WriteString(entry.Error.Error())
		if !f.NoColor {
			b.WriteString(colorReset)
		}
	}

	b.WriteString("\n")

	if entry.StackTrace != "" {
		if !f.NoColor {
			b.WriteString(colorDim)
		}
		b.WriteString(entry.StackTrace)
		if !f.NoColor {
			b.WriteString(colorReset)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (f *TextFormatter) getMessageColor(level LogLevel) string {
	if f.NoColor {
		return ""
	}
	switch level {
	case ErrorLevel, FatalLevel, PanicLevel:
		return colorWhite
	default:
		return ""
	}
}

func (f *TextFormatter) getLevelColor(level LogLevel) string {
	if f.NoColor {
		return ""
	}
	switch level {
	case TraceLevel:
		return colorMagenta
	case DebugLevel:
		return colorBlue
	case InfoLevel:
		return colorGreen
	case WarnLevel:
		return colorYellow
	case ErrorLevel:
		return colorRed
	case FatalLevel:
		return colorBrightRed
	case PanicLevel:
		return colorBrightRed
	default:
		return ""
	}
}

// JSONFormatter formats logs as JSON
type JSONFormatter struct {
	PrettyPrint     bool
	TimestampFormat string
}

func (f *JSONFormatter) Format(entry *LogEntry) string {
	data := make(map[string]any)

	timestampFormat := f.TimestampFormat
	if timestampFormat == "" {
		timestampFormat = time.RFC3339Nano
	}

	data["timestamp"] = entry.Time.Format(timestampFormat)
	data["level"] = entry.Level.String()
	data["message"] = entry.Message

	if entry.Prefix != "" {
		data["prefix"] = entry.Prefix
	}
	if entry.File != "" {
		data["caller"] = fmt.Sprintf("%s:%d", entry.File, entry.Line)
	}
	if entry.Function != "" {
		data["function"] = entry.Function
	}
	if entry.Error != nil {
		data["error"] = entry.Error.Error()
	}
	if len(entry.Fields) > 0 {
		maps.Copy(data, entry.Fields)
	}
	if entry.StackTrace != "" {
		data["stack_trace"] = entry.StackTrace
	}

	var output []byte
	var err error
	if f.PrettyPrint {
		output, err = json.MarshalIndent(data, "", "  ")
	} else {
		output, err = json.Marshal(data)
	}

	if err != nil {
		return fmt.Sprintf(`{"error":"failed to marshal log entry: %v"}`+"\n", err)
	}

	return string(output) + "\n"
}
