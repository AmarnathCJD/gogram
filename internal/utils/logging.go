// Copyright (c) 2024 RoseLoverX

package utils

import (
	"fmt"
	"log"
	"runtime"
	"strings"
)

const (
	// DebugLevel is the lowest level of logging
	DebugLevel = iota
	// InfoLevel is the second lowest level of logging
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
	colorOff    = []byte("\033[0m")
	colorRed    = []byte("\033[0;31m")
	colorGreen  = []byte("\033[0;32m")
	colorOrange = []byte("\033[0;33m")
	colorPurple = []byte("\033[0;35m")
	colorCyan   = []byte("\033[0;36m")
)

// Logger is the logging struct.
type Logger struct {
	Level   int
	Prefix  string
	nocolor bool
}

// NoColor disables colorized output.
func (l *Logger) NoColor() {
	l.nocolor = true
}

// Color enables colorized output. (default)
func (l *Logger) Color() {
	l.nocolor = false
}

func (l *Logger) colorize(color []byte, s string) string {
	if l.nocolor {
		return s
	}
	return string(color) + s + string(colorOff)
}

func (l *Logger) Lev() string {
	switch l.Level {
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarnLevel:
		return "warn"
	case ErrorLevel:
		return "error"
	case NoLevel:
		return "disabled"
	case TraceLevel:
		return "trace"
	default:
		return "info"
	}
}

// SetLevelString sets the level string
func (l *Logger) SetLevel(level string) *Logger {
	switch level {
	case "debug":
		l.Level = DebugLevel
	case "info":
		l.Level = InfoLevel
	case "warn":
		l.Level = WarnLevel
	case "error":
		l.Level = ErrorLevel
	case "disabled":
		l.Level = NoLevel
	default:
		l.Level = InfoLevel
	}
	return l
}

// Log logs a message at the given level.
func (l *Logger) Error(v ...any) {
	// TODO: runtime.Caller(1)
	if l.Level <= ErrorLevel {
		log.Println(l.colorize(colorRed, "[error]"), l.Prefix, "-", getVariable(v...))
	}
}

func (l *Logger) Warn(v ...any) {
	if l.Level <= WarnLevel {
		log.Println(l.colorize(colorOrange, "[warn] "), l.Prefix, "-", getVariable(v...))
	}
}

func (l *Logger) Info(v ...any) {
	if l.Level <= InfoLevel {
		log.Println(l.colorize(colorGreen, "[info] "), l.Prefix, "-", getVariable(v...))
	}
}

func (l *Logger) Debug(v ...any) {
	if l.Level <= DebugLevel {
		log.Println(l.colorize(colorPurple, "[debug]"), l.Prefix, "-", getVariable(v...))
	}
}

func (l *Logger) Trace(v ...any) {
	if l.Level <= TraceLevel {
		log.Println(l.colorize(colorCyan, "[trace]"), l.Prefix, "-", getVariable(v...))
	}
}

func (l *Logger) Panic(v ...any) {
	stack := make([]byte, 1536)
	runtime.Stack(stack, false)

	log.Println(l.colorize(colorCyan, "[panic]"), l.Prefix, "-", getVariable(v...), "\n", l.colorize(colorOrange, string(stack)))
}

// NewLogger returns a new Logger instance.
func NewLogger(prefix string) *Logger {
	return &Logger{
		Prefix: prefix,
	}
}

func getVariable(v ...any) string {
	if len(v) == 0 {
		return ""
	}
	if len(v) == 1 {
		return fmt.Sprint(v[0])
	}
	return strings.Trim(fmt.Sprint(v...), "[]")
}
