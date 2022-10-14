package utils

import "log"

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
		log.Println(l.Prefix, v)
	}
}

func (l *Logger) Warn(v ...any) {
	if l.Level <= WarnLevel {
		log.Println(l.Prefix, v)
	}
}

func (l *Logger) Info(v ...any) {
	if l.Level <= InfoLevel {
		log.Println(l.Prefix, v)
	}
}

func (l *Logger) Debug(v ...any) {
	if l.Level <= DebugLevel {
		log.Println(l.Prefix, v)
	}
}

// NewLogger returns a new Logger instance.
func NewLogger(prefix string) *Logger {
	return &Logger{
		Prefix: prefix,
	}
}
