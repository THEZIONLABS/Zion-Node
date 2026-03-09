package logger

import (
	"github.com/sirupsen/logrus"
)

// NewLogrusLogger creates a new logrus logger with specified level
func NewLogrusLogger(level string) *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(GetLogLevel(level))
	return logger
}

// GetLogLevel converts log level string to logrus level
func GetLogLevel(level string) logrus.Level {
	switch level {
	case "debug":
		return logrus.DebugLevel
	case "info":
		return logrus.InfoLevel
	case "warn":
		return logrus.WarnLevel
	case "error":
		return logrus.ErrorLevel
	default:
		return logrus.InfoLevel
	}
}
