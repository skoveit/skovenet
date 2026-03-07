package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

var (
	currentLevel Level = LevelInfo
	mu           sync.RWMutex
	output       io.Writer = os.Stderr
)

func init() {
	// Standard logger setup: remove default flags and prefix
	log.SetFlags(log.LstdFlags)
	log.SetOutput(output)
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// SetLevel set the minimum log level to display
func SetLevel(l Level) {
	mu.Lock()
	defer mu.Unlock()
	currentLevel = l
}

// SetOutput sets the output writer
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	output = w
	log.SetOutput(w)
}

func logf(l Level, format string, v ...interface{}) {
	mu.RLock()
	lvl := currentLevel
	mu.RUnlock()

	if l >= lvl {
		msg := fmt.Sprintf(format, v...)
		log.Printf("[%s] %s", l.String(), msg)
	}
}

// Debug logs a message at debug level
func Debug(format string, v ...interface{}) {
	logf(LevelDebug, format, v...)
}

// Info logs a message at info level
func Info(format string, v ...interface{}) {
	logf(LevelInfo, format, v...)
}

// Warn logs a message at warn level
func Warn(format string, v ...interface{}) {
	logf(LevelWarn, format, v...)
}

// Error logs a message at error level
func Error(format string, v ...interface{}) {
	logf(LevelError, format, v...)
}

// Fatal logs a message at fatal level and exits
func Fatal(format string, v ...interface{}) {
	logf(LevelFatal, format, v...)
	os.Exit(1)
}

// Legacy support (to be refactored out)
func Debugln(v ...interface{}) {
	Debug("%v", v...)
}
func SetDebug(enabled bool) {
	if enabled {
		SetLevel(LevelDebug)
	} else {
		SetLevel(LevelInfo)
	}
}
