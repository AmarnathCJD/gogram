package utils

import (
	"fmt"
	"log"
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
)

// Logger is the logging struct.
type Logger struct {
	Level  int
	Prefix string
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
	if l.Level <= ErrorLevel {
		log.Println(l.Prefix, "- <error> -", getVariable(v...))
	}
}

func (l *Logger) Warn(v ...any) {
	if l.Level <= WarnLevel {
		log.Println(l.Prefix, "- <warn> -", getVariable(v...))
	}
}

func (l *Logger) Info(v ...any) {
	if l.Level <= InfoLevel {
		log.Println(l.Prefix, "- <info> -", getVariable(v...))
	}
}

func (l *Logger) Debug(v ...any) {
	if l.Level <= DebugLevel {
		log.Println(l.Prefix, "- <debug> -", getVariable(v...))
	}
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
