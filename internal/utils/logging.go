// Copyright (c) 2024 RoseLoverX

package utils

import (
	"fmt"
	"log"
	"runtime"
	"strings"
)

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
	colorOff    = []byte("\033[0m")
	colorRed    = []byte("\033[0;31m")
	colorGreen  = []byte("\033[0;32m")
	colorOrange = []byte("\033[0;33m")
	colorPurple = []byte("\033[0;35m")
	colorCyan   = []byte("\033[0;36m")
)

// Logger is the logging struct.
type Logger struct {
	Level   LogLevel
	Prefix  string
	nocolor bool
}

// NoColor disables colorized output.
func (l *Logger) NoColor(nocolor ...bool) *Logger {
	if len(nocolor) > 0 {
		l.nocolor = nocolor[0]
	} else {
		l.nocolor = true
	}

	return l
}

// Color enables colorized output. (default)
func (l *Logger) Color() bool {
	return !l.nocolor
}

func (l *Logger) SetPrefix(prefix string) *Logger {
	l.Prefix = prefix
	return l
}

func (l *Logger) colorize(color []byte, s string) string {
	if l.nocolor {
		return s
	}
	return string(color) + s + string(colorOff)
}

func (l *Logger) Lev() LogLevel {
	return l.Level
}

// SetLevelString sets the level string
func (l *Logger) SetLevel(level LogLevel) *Logger {
	l.Level = level
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
	stack := make([]byte, 2048)
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
	return strings.Trim(fmt.Sprint(v...), "]")
}
