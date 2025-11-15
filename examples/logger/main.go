package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/amarnathcjd/gogram/telegram"
)

// MySimpleLogger implements telegram.SimpleLogger with just 8 basic methods
type MySimpleLogger struct {
	logger          *log.Logger
	level           telegram.LogLevel
	timestampFormat string
	output          io.Writer
}

func NewMySimpleLogger() *MySimpleLogger {
	return &MySimpleLogger{
		logger: log.New(os.Stdout, "", log.LstdFlags),
		level:  telegram.InfoLevel,
		output: os.Stdout,
	}
}

// Implement the 8 required methods
func (l *MySimpleLogger) Debug(msg any, args ...any) {
	l.logger.Printf("[DEBUG] %v %v\n", msg, args)
}

func (l *MySimpleLogger) Info(msg any, args ...any) {
	l.logger.Printf("[INFO] %v %v\n", msg, args)
}

func (l *MySimpleLogger) Warn(msg any, args ...any) {
	l.logger.Printf("[WARN] %v %v\n", msg, args)
}

func (l *MySimpleLogger) Error(msg any, args ...any) {
	l.logger.Printf("[ERROR] %v %v\n", msg, args)
}

func (l *MySimpleLogger) SetLevel(level telegram.LogLevel) {
	l.level = level
}

func (l *MySimpleLogger) GetLevel() telegram.LogLevel {
	return l.level
}

func (l *MySimpleLogger) SetOutput(w any) {
	if writer, ok := w.(io.Writer); ok {
		l.output = writer
		l.logger.SetOutput(writer)
	}
}

func (l *MySimpleLogger) GetOutput() any {
	return l.output
}

func (l *MySimpleLogger) SetTimestampFormat(format string) {
	l.timestampFormat = format
	// Update logger flags if needed
}

func main() {
	// Create your custom logger
	myLogger := NewMySimpleLogger()

	// Wrap it to work with gogram
	wrappedLogger := telegram.WrapSimpleLogger(myLogger)

	// Use it with gogram client
	client, err := telegram.NewClient(telegram.ClientConfig{
		AppID:   6,
		AppHash: "app_hash",
		Logger:  wrappedLogger, // Pass the wrapped logger
	})

	if err != nil {
		panic(err)
	}

	// Your logger will now handle all gogram logs
	client.Log.Info("Client initialized with custom logger!")
	client.Log.Debug("This is a debug message")
	client.Log.Warn("This is a warning")
	client.Log.Error("This is an error")

	fmt.Println("\nSimpleLogger requires 8 methods:")
	fmt.Println("- Debug(msg, args...)")
	fmt.Println("- Info(msg, args...)")
	fmt.Println("- Warn(msg, args...)")
	fmt.Println("- Error(msg, args...)")
	fmt.Println("- SetLevel(level)")
	fmt.Println("- GetLevel()")
	fmt.Println("- SetOutput(w)")
	fmt.Println("- GetOutput()")
	fmt.Println("- SetTimestampFormat(format)")
	fmt.Println("\nJust wrap with: telegram.WrapSimpleLogger(yourLogger)")
}
