package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)
i
type LogLevel int

const (
	// DebugLevel is the lowest level of logging
	DebugLevel LogLevel = iota + 1
	// InfoLevel is the second lowest level of logging (default)
	InfoLevel
	// WarnLevel is the third highest level of logging
	WarnLevel
	// ErrorLevel is the highest level of logging
	ErrorLevel
	// NoLevel disables all logging
	NoLevel
	// TraceLevel is the highest level of logging
	TraceLevel
)

var (
	boldOn      = "\033[1m"
	boldOff     = "\033[22m"
	colorOff    = "\033[0m"
	colorRed    = "\033[0;31m"
	colorGreen  = "\033[0;32m"
	colorOrange = "\033[0;33m"
	colorPurple = "\033[0;35m"
	colorCyan   = "\033[0;36m"
)

type Logger struct {
	mut      sync.Mutex
	Level    LogLevel
	Prefix   string
	noColor  bool
	noBold   bool
	jsonMode bool
	writer   *bufio.Writer
	output   io.Writer
}

func NewLogger(prefix string) *Logger {
	return &Logger{
		Prefix: prefix,
		Level:  InfoLevel,
		output: os.Stdout,
		writer: bufio.NewWriter(os.Stdout),
	}
}

// SetOutput allows logging to a file, network, or memory writer.
func (l *Logger) SetOutput(w io.Writer) *Logger {
	l.mut.Lock()
	defer l.mut.Unlock()
	l.output = w
	l.writer = bufio.NewWriter(w)
	return l
}

// Flush ensures all buffered logs are written out.
func (l *Logger) Flush() {
	l.mut.Lock()
	defer l.mut.Unlock()
	l.writer.Flush()
}

// Set or Unset NoColor mode
func (l *Logger) NoColor() *Logger {
	l.noColor = !l.noColor
	return l
}

func (l *Logger) NoBold() *Logger {
	l.noBold = !l.noBold
	return l
}

func (l *Logger) EnableJSON(enabled bool) *Logger {
	l.jsonMode = enabled
	return l
}

func (l *Logger) SetLevel(level LogLevel) *Logger {
	l.Level = level
	return l
}

func (l *Logger) SetPrefix(prefix string) *Logger {
	l.Prefix = prefix
	return l
}

func (l *Logger) colorize(color, s string) string {
	if l.noColor {
		return s
	}
	return color + s + colorOff
}

func (l *Logger) bold() string {
	if l.noBold {
		return l.Prefix
	}
	return boldOn + l.Prefix + boldOff
}

func (l *Logger) write(s string) {
	l.mut.Lock()
	defer l.mut.Unlock()
	l.writer.WriteString(s)
	l.writer.Flush()
}

func getVariable(v ...any) string {
	if len(v) == 0 {
		return ""
	}
	if len(v) == 1 {
		return fmt.Sprint(v[0])
	}
	return fmt.Sprint(v...)
}

func (l *Logger) log(level LogLevel, color, label string, v ...any) {
	if l.Level > level {
		return
	}

	msg := getVariable(v...)
	_, file, line, _ := runtime.Caller(2)
	shortFile := file[strings.LastIndex(file, "/")+1:]
	timestamp := time.Now().Format("2006-01-02 15:04:05")

	if l.jsonMode {
		entry := map[string]any{
			"time":   timestamp,
			"level":  strings.Trim(label, "[]"),
			"prefix": l.Prefix,
			"file":   fmt.Sprintf("%s:%d", shortFile, line),
			"msg":    msg,
		}
		data, _ := json.Marshal(entry)
		l.write(string(data) + "\n")
	} else {
		formatted := fmt.Sprintf("%s %s %s %-2s %s:%d - %s\n",
			timestamp, l.colorize(color, fmt.Sprintf("%-*s", 7, label)),
			l.bold(),
			"",
			shortFile,
			line,
			msg)
		l.write(formatted)
	}
}

func (l *Logger) Error(v ...any) { l.log(ErrorLevel, colorRed, "[error]", v...) }
func (l *Logger) Warn(v ...any)  { l.log(WarnLevel, colorOrange, "[warn]", v...) }
func (l *Logger) Info(v ...any)  { l.log(InfoLevel, colorGreen, "[info]", v...) }
func (l *Logger) Debug(v ...any) { l.log(DebugLevel, colorPurple, "[debug]", v...) }
func (l *Logger) Trace(v ...any) { l.log(TraceLevel, colorCyan, "[trace]", v...) }

func (l *Logger) Panic(v ...any) {
	stack := make([]byte, 4096)
	runtime.Stack(stack, false)
	l.log(ErrorLevel, colorCyan, "[panic]", fmt.Sprint(v...))
	l.write(string(stack))
	panic(fmt.Sprint(v...))
}
