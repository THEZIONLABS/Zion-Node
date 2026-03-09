package testutil

import (
	"github.com/sirupsen/logrus"
)

// NewTestLogger creates a test logger
func NewTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return logger
}
