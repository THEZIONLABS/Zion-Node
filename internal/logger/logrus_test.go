package logger

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNewLogrusLogger(t *testing.T) {
	tests := []struct {
		level    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"unknown", logrus.InfoLevel}, // default
		{"", logrus.InfoLevel},        // empty string default
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger := NewLogrusLogger(tt.level)
			if logger == nil {
				t.Fatal("NewLogrusLogger returned nil")
			}
			if logger.GetLevel() != tt.expected {
				t.Errorf("Expected level %v for input %q, got %v", tt.expected, tt.level, logger.GetLevel())
			}
		})
	}
}

func TestGetLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"unknown", logrus.InfoLevel},
		{"DEBUG", logrus.InfoLevel}, // Case-sensitive, should default
		{"", logrus.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := GetLogLevel(tt.input)
			if level != tt.expected {
				t.Errorf("GetLogLevel(%q) = %v, want %v", tt.input, level, tt.expected)
			}
		})
	}
}

func TestNewLogrusLogger_CanLog(t *testing.T) {
	logger := NewLogrusLogger("debug")
	// Should not panic
	logger.Debug("test debug message")
	logger.Info("test info message")
	logger.Warn("test warn message")
}
