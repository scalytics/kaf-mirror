// Copyright 2025 Scalytics, Inc. and Scalytics Europe, LTD
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
	FATAL: "FATAL",
}

type Logger struct {
	mu          sync.Mutex
	debugLogger *log.Logger
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	fatalLogger *log.Logger
	level       Level
	file        *os.File
}

var defaultLogger *Logger
var once sync.Once

func init() {
	// Initialize a minimal logger to stdout.
	// The main application will re-initialize this with the proper configuration.
	defaultLogger = &Logger{
		level:       INFO,
		debugLogger: log.New(io.Discard, "", 0),
		infoLogger:  log.New(os.Stdout, "", 0),
		warnLogger:  log.New(os.Stdout, "", 0),
		errorLogger: log.New(os.Stderr, "", 0),
		fatalLogger: log.New(os.Stderr, "", 0),
	}
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	// Get caller info
	_, file, line, ok := runtime.Caller(2)
	var caller string
	if ok {
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	} else {
		caller = "unknown"
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	levelName := levelNames[level]
	message := fmt.Sprintf(format, args...)

	logLine := fmt.Sprintf("[%s] %-5s [%s] %s", timestamp, levelName, caller, message)

	switch level {
	case DEBUG:
		l.debugLogger.Println(logLine)
	case INFO:
		l.infoLogger.Println(logLine)
	case WARN:
		l.warnLogger.Println(logLine)
	case ERROR:
		l.errorLogger.Println(logLine)
	case FATAL:
		l.fatalLogger.Println(logLine)
		os.Exit(1)
	}
}

// logWithTag logs a message with AI analysis tags
func (l *Logger) logWithTag(level Level, category, subcategory, jobID string, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	// Get caller info
	_, file, line, ok := runtime.Caller(2)
	var caller string
	if ok {
		caller = fmt.Sprintf("%s:%d", filepath.Base(file), line)
	} else {
		caller = "unknown"
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	levelName := levelNames[level]
	message := fmt.Sprintf(format, args...)

	// Format: [timestamp] LEVEL [caller] [AI:category:subcategory] [job:jobID] message
	var logLine string
	if jobID != "" {
		logLine = fmt.Sprintf("[%s] %-5s [%s] [AI:%s:%s] [job:%s] %s",
			timestamp, levelName, caller, category, subcategory, jobID, message)
	} else {
		logLine = fmt.Sprintf("[%s] %-5s [%s] [AI:%s:%s] %s",
			timestamp, levelName, caller, category, subcategory, message)
	}

	switch level {
	case DEBUG:
		l.debugLogger.Println(logLine)
	case INFO:
		l.infoLogger.Println(logLine)
	case WARN:
		l.warnLogger.Println(logLine)
	case ERROR:
		l.errorLogger.Println(logLine)
	case FATAL:
		l.fatalLogger.Println(logLine)
		os.Exit(1)
	}
}

func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(DEBUG, format, args...)
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(INFO, format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(WARN, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(ERROR, format, args...)
}

func (l *Logger) Fatal(format string, args ...interface{}) {
	l.log(FATAL, format, args...)
}

func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// Package-level functions using default logger
func Debug(format string, args ...interface{}) {
	defaultLogger.Debug(format, args...)
}

func Info(format string, args ...interface{}) {
	defaultLogger.Info(format, args...)
}

func Warn(format string, args ...interface{}) {
	defaultLogger.Warn(format, args...)
}

func Error(format string, args ...interface{}) {
	defaultLogger.Error(format, args...)
}

func Fatal(format string, args ...interface{}) {
	defaultLogger.Fatal(format, args...)
}

func Close() error {
	return defaultLogger.Close()
}

// AI-tagged logging functions for operational intelligence
func InfoAI(category, subcategory, jobID, format string, args ...interface{}) {
	defaultLogger.logWithTag(INFO, category, subcategory, jobID, format, args...)
}

func WarnAI(category, subcategory, jobID, format string, args ...interface{}) {
	defaultLogger.logWithTag(WARN, category, subcategory, jobID, format, args...)
}

func ErrorAI(category, subcategory, jobID, format string, args ...interface{}) {
	defaultLogger.logWithTag(ERROR, category, subcategory, jobID, format, args...)
}

func DebugAI(category, subcategory, jobID, format string, args ...interface{}) {
	defaultLogger.logWithTag(DEBUG, category, subcategory, jobID, format, args...)
}

// SetLevel sets the logging level for the default logger
func SetLevel(level Level) {
	defaultLogger.level = level
}

func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.debugLogger.SetOutput(w)
	defaultLogger.infoLogger.SetOutput(w)
	defaultLogger.warnLogger.SetOutput(w)
	defaultLogger.errorLogger.SetOutput(w)
	defaultLogger.fatalLogger.SetOutput(w)
}

// GetProductionLogDir returns the appropriate log directory for production
func GetProductionLogDir() string {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// Check if we have write access to /var/log
		if err := os.MkdirAll("/var/log/kaf-mirror", 0755); err == nil {
			// Test write access
			testFile := "/var/log/kaf-mirror/.test"
			if f, err := os.Create(testFile); err == nil {
				f.Close()
				os.Remove(testFile)
				return "/var/log/kaf-mirror"
			}
		}
	}

	// Fallback to local logs directory
	return "logs"
}

// ParseLevel converts a string to a log level
func ParseLevel(levelStr string) (Level, error) {
	switch strings.ToUpper(levelStr) {
	case "DEBUG":
		return DEBUG, nil
	case "INFO":
		return INFO, nil
	case "WARN":
		return WARN, nil
	case "ERROR":
		return ERROR, nil
	case "FATAL":
		return FATAL, nil
	default:
		return INFO, fmt.Errorf("unknown log level: %s", levelStr)
	}
}

// InitializeFromConfig initializes the default logger from configuration.
// This function is designed to be called only once from main.
func InitializeFromConfig(logFile string, levelStr string, enableConsole bool) error {
	var finalErr error
	once.Do(func() {
		level, err := ParseLevel(levelStr)
		if err != nil {
			finalErr = fmt.Errorf("invalid log level: %v", err)
			return
		}

		// Ensure the log directory exists
		logDir := filepath.Dir(logFile)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			finalErr = fmt.Errorf("failed to create log directory: %v", err)
			return
		}

		// Open the configured log file in append mode
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			finalErr = fmt.Errorf("failed to open log file: %v", err)
			return
		}

		// Create writer - file only or file + console
		var writer io.Writer = file
		if enableConsole {
			writer = io.MultiWriter(file, os.Stdout)
		}

		defaultLogger.mu.Lock()
		defer defaultLogger.mu.Unlock()

		defaultLogger.level = level
		defaultLogger.file = file
		defaultLogger.debugLogger = log.New(writer, "", 0)
		defaultLogger.infoLogger = log.New(writer, "", 0)
		defaultLogger.warnLogger = log.New(writer, "", 0)
		defaultLogger.errorLogger = log.New(writer, "", 0)
		defaultLogger.fatalLogger = log.New(writer, "", 0)
	})
	return finalErr
}
