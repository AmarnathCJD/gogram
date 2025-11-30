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

	// ============================================================
	// BUILT-IN LOGGER EXAMPLE
	// ============================================================
	fmt.Println("\n==================================================")
	fmt.Println("BUILT-IN LOGGER OPTIONS")
	fmt.Println("==")

	// Option 1: Use default logger (simplest)
	defaultLogger := telegram.NewDefaultLogger("myapp")
	defaultLogger.Info("Using default logger")

	// Option 2: Create logger with level only
	simpleLogger := telegram.NewLogger(telegram.DebugLevel)
	simpleLogger.Debug("Debug message visible")

	// Option 3: Create logger with full configuration
	fullConfig := &telegram.LoggerConfig{
		Level:           telegram.DebugLevel, // Log level (Trace, Debug, Info, Warn, Error, Fatal, Panic)
		Prefix:          "gogram",            // Prefix shown in log output
		Output:          os.Stdout,           // Output writer (os.Stdout, file, etc.)
		Color:           true,                // Enable colored output
		ShowCaller:      true,                // Show file:line in logs
		ShowFunction:    false,               // Show function name in logs
		FullStackTrace:  false,               // false = formatted stack, true = raw full stack
		TimestampFormat: "15:04:05",          // Time format (Go time layout)
		BufferSize:      4096,                // Write buffer size
		AsyncMode:       false,               // Enable async logging
		AsyncQueueSize:  1000,                // Async queue size (if AsyncMode)
		JSONOutput:      false,               // Output as JSON instead of text
		ErrorHandler:    nil,                 // Custom error handler
	}
	configuredLogger := telegram.NewLoggerWithConfig(fullConfig)
	configuredLogger.Info("Configured logger ready!")

	// Option 4: Use with gogram client
	clientWithLogger, _ := telegram.NewClient(telegram.ClientConfig{
		AppID:   6,
		AppHash: "app_hash",
		Logger:  configuredLogger,
	})

	// Runtime configuration changes
	clientWithLogger.Log.SetLevel(telegram.DebugLevel) // Change log level
	clientWithLogger.Log.SetColor(true)                // Enable colors
	clientWithLogger.Log.ShowCaller(true)              // Show file:line
	clientWithLogger.Log.ShowFunction(true)            // Show function names
	clientWithLogger.Log.FullStackTrace(false)         // Use formatted panic stack (default)
	clientWithLogger.Log.SetTimestampFormat("2006-01-02 15:04:05")
	clientWithLogger.Log.SetJSONMode(false) // Text mode (default)

	// Contextual logging
	clientWithLogger.Log.WithField("user_id", 12345).Info("User action")
	clientWithLogger.Log.WithFields(map[string]any{
		"chat_id": -100123,
		"action":  "send_message",
	}).Debug("Chat operation")
	clientWithLogger.Log.WithError(fmt.Errorf("connection timeout")).Error("Failed to connect")

	// Different log levels
	clientWithLogger.Log.Trace("Trace message (most verbose)")
	clientWithLogger.Log.Debug("Debug message")
	clientWithLogger.Log.Info("Info message")
	clientWithLogger.Log.Warn("Warning message")
	clientWithLogger.Log.Error("Error message")
	// clientWithLogger.Log.Fatal("Fatal message") // Exits program
	// clientWithLogger.Log.Panic("Panic message") // Logs panic with stack trace

	fmt.Println("\nBuilt-in Logger Features:")
	fmt.Println("- Multiple log levels (Trace → Panic)")
	fmt.Println("- Colored output support")
	fmt.Println("- Caller info (file:line, function)")
	fmt.Println("- Contextual fields (WithField, WithFields)")
	fmt.Println("- JSON or Text output modes")
	fmt.Println("- Async logging option")
	fmt.Println("- Formatted panic stack traces")
	fmt.Println("- File rotation support")
}
