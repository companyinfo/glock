package glock

import "github.com/go-logr/logr"

// LockLogger holds the global logger.
type LockLogger struct {
	logger logr.Logger
}

var globalLogger *LockLogger

// InitializeLogger sets up logger with a user-defined or default logger.
func InitializeLogger(l logr.Logger) {
	globalLogger = &LockLogger{
		logger: l,
	}
}

// GetLogger returns the global Logger instance.
func GetLogger() logr.Logger {
	return globalLogger.logger
}
