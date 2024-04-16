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
			l.logWin("error", v...)
		} else {
			log.Println(l.Prefix, "-", string("\033[35m")+"error"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Warn(v ...any) {
	if l.Level <= WarnLevel {
		if l.WinTerminal() {
			l.logWin("warn", v...)
		} else {
			log.Println(l.Prefix, "-", string("\033[33m")+"warning"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Info(v ...any) {
	if l.Level <= InfoLevel {
		if l.WinTerminal() {
			l.logWin("info", v...)
		} else {
			log.Println(l.Prefix, "-", string("\033[31m")+"info"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Debug(v ...any) {
	if l.Level <= DebugLevel {
		if l.WinTerminal() {
			l.logWin("debug", v...)
		} else {
			log.Println(l.Prefix, "-", string("\033[32m")+"debug"+string("\033[0m"), "-", getVariable(v...))
		}
	}
}

func (l *Logger) Panic(v ...any) {
	if l.WinTerminal() {
		l.logWin("panic", v...)
	} else {
		log.Println(l.Prefix, "-", string("\033[31m")+"panic"+string("\033[0m"), "-", getVariable(v...))
	}
}

func (l *Logger) WinTerminal() bool {
	return runtime.GOOS == "windows"
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

func (l *Logger) levelColor(lev string) string {
	lev = strings.ToLower(lev)
	if lev == "debug" {
		return "Green"
	} else if lev == "info" {
		return "Magenta"
	} else if lev == "warn" {
		return "Yellow"
	} else if lev == "error" {
		return "Red"
	} else if lev == "panic" {
		return "Cyan"
	}
	return "White"
}

// ASCII Is a pain in Windows Terminal,
// So we use PowerShell to print the logs
func (l *Logger) logWin(lev string, v ...any) {
	log.Println(l.Prefix, "-", lev, "-", getVariable(v...))
	// Disable PowerShell Logging, it's not working properly
	// cmdPref := "Write-Host -NoNewline -ForegroundColor %s '%s '; Write-Host -NoNewline '%s - '; Write-Host -NoNewline -ForegroundColor %s '%s'; Write-Host -ForegroundColor White ' - %s'"
	// cmdPref = fmt.Sprintf(cmdPref, l.levelColor(lev), time.Now().Format("2006/01/02 15:04:05"), l.Prefix, l.levelColor(lev), lev, getVariable(v...))
	// if _, err := exec.LookPath("powershell"); err == nil {
	// 	cmd := exec.Command("powershell", "-Command", cmdPref)
	// 	cmd.Stdout = os.Stdout
	// 	cmd.Run()
	// } else {
	// 	log.Println(l.Prefix, "-", lev, "-", getVariable(v...))
	// }
}
