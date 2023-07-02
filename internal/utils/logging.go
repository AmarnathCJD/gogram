package utils

import (
	"fmt"
	"log"
	"os"
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
		if l.WinTerminal() {
			log.Println(l.Prefix, "- Error -", getVariable(v...))
		} else {
			log.Println(l.Prefix, "-", string("\033[35m")+"Error"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Warn(v ...any) {
	if l.Level <= WarnLevel {
		if l.WinTerminal() {
			log.Println(l.Prefix, "- Warn -", getVariable(v...))
		} else {
			log.Println(l.Prefix, "-", string("\033[33m")+"Warn"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Info(v ...any) {
	if l.Level <= InfoLevel {
		if l.WinTerminal() {
			log.Println(l.Prefix, "-", "Info", "-", getVariable(v...))
		} else {
			log.Println(l.Prefix, "-", string("\033[31m")+"Info"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Debug(v ...any) {
	if l.Level <= DebugLevel {
		if l.WinTerminal() {
			log.Println(l.Prefix, "-", "Debug", "-", getVariable(v...))
		} else {
			log.Println(l.Prefix, "-", string("\033[32m")+"Debug"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Panic(v ...any) {
	if l.WinTerminal() {
		log.Println(l.Prefix, "-", "Panic", "-", getVariable(v...))
	} else {
		log.Println(l.Prefix, "-", string("\033[31m")+"Panic"+string("\033[0m"), "-", getVariable(v...))
	}
}

func (l *Logger) WinTerminal() bool {
	if runtime.GOOS == "windows" && os.Getenv("TERM_PROGRAM") != "vscode" {
		return true
	}
	return false
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
