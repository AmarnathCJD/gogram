package utils

import "log"

const (
	// DebugLevel is the lowest level of logging
	DebugLevel = iota
	// InfoLevel is the second lowest level of logging
	InfoLevel = iota
	// WarnLevel is the third highest level of logging
	WarnLevel = iota
	// ErrorLevel is the highest level of logging
	ErrorLevel = iota
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
	default:
		l.Level = InfoLevel
	}
	return l
}

// Log logs a message at the given level.
func (l *Logger) Error(msg interface{}) {
	if l.Level <= ErrorLevel {
		log.Printf("%s - ERROR - %s", l.Prefix, msg)
	}
}

func (l *Logger) Warn(msg interface{}) {
	if l.Level <= WarnLevel {
		log.Printf("%s - WARN - %s", l.Prefix, msg)
	}
}

func (l *Logger) Info(msg interface{}) {
	if l.Level <= InfoLevel {
		log.Printf("%s - INFO - %s", l.Prefix, msg)
	}
}

func (l *Logger) Debug(msg interface{}) {
	if l.Level <= DebugLevel {
		log.Printf("%s - DEBUG - %s", l.Prefix, msg)
	}
}

// NewLogger returns a new Logger instance.
func NewLogger(prefix string) *Logger {
	return &Logger{
		Prefix: prefix,
	}
}
