// Package logger provides structured logging for DomiClaw.
package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

var levelColors = map[Level]string{
	LevelDebug: "\033[36m", // Cyan
	LevelInfo:  "\033[32m", // Green
	LevelWarn:  "\033[33m", // Yellow
	LevelError: "\033[31m", // Red
}

const colorReset = "\033[0m"

// Logger is a simple structured logger.
type Logger struct {
	mu       sync.Mutex
	level    Level
	output   io.Writer
	useColor bool
}

var defaultLogger = &Logger{
	level:    LevelInfo,
	output:   os.Stderr,
	useColor: true,
}

// SetLevel sets the minimum log level.
func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// SetOutput sets the output writer.
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.output = w
}

// SetColor enables or disables colored output.
func SetColor(enabled bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.useColor = enabled
}

func (l *Logger) log(level Level, component, message string, fields map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	timestamp := time.Now().Format("15:04:05")
	levelName := levelNames[level]

	var prefix, suffix string
	if l.useColor {
		prefix = levelColors[level]
		suffix = colorReset
	}

	// Format: [TIME] LEVEL [component] message {fields}
	line := fmt.Sprintf("[%s] %s%5s%s", timestamp, prefix, levelName, suffix)
	if component != "" {
		line += fmt.Sprintf(" [%s]", component)
	}
	line += " " + message

	if len(fields) > 0 {
		line += " {"
		first := true
		for k, v := range fields {
			if !first {
				line += ", "
			}
			line += fmt.Sprintf("%s=%v", k, v)
			first = false
		}
		line += "}"
	}

	fmt.Fprintln(l.output, line)
}

// Debug logs a debug message.
func Debug(message string) {
	defaultLogger.log(LevelDebug, "", message, nil)
}

// DebugF logs a debug message with fields.
func DebugF(message string, fields map[string]interface{}) {
	defaultLogger.log(LevelDebug, "", message, fields)
}

// DebugCF logs a debug message with component and fields.
func DebugCF(component, message string, fields map[string]interface{}) {
	defaultLogger.log(LevelDebug, component, message, fields)
}

// Info logs an info message.
func Info(message string) {
	defaultLogger.log(LevelInfo, "", message, nil)
}

// InfoF logs an info message with fields.
func InfoF(message string, fields map[string]interface{}) {
	defaultLogger.log(LevelInfo, "", message, fields)
}

// InfoCF logs an info message with component and fields.
func InfoCF(component, message string, fields map[string]interface{}) {
	defaultLogger.log(LevelInfo, component, message, fields)
}

// Warn logs a warning message.
func Warn(message string) {
	defaultLogger.log(LevelWarn, "", message, nil)
}

// WarnF logs a warning message with fields.
func WarnF(message string, fields map[string]interface{}) {
	defaultLogger.log(LevelWarn, "", message, fields)
}

// WarnCF logs a warning message with component and fields.
func WarnCF(component, message string, fields map[string]interface{}) {
	defaultLogger.log(LevelWarn, component, message, fields)
}

// Error logs an error message.
func Error(message string) {
	defaultLogger.log(LevelError, "", message, nil)
}

// ErrorF logs an error message with fields.
func ErrorF(message string, fields map[string]interface{}) {
	defaultLogger.log(LevelError, "", message, fields)
}

// ErrorCF logs an error message with component and fields.
func ErrorCF(component, message string, fields map[string]interface{}) {
	defaultLogger.log(LevelError, component, message, fields)
}
