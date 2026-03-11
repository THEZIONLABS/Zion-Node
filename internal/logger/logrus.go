package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

// NewLogrusLogger creates a new logrus logger with specified level
func NewLogrusLogger(level string) *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(GetLogLevel(level))
	return logger
}

// SetupFileLogging adds a log file writer to the logger.
// Logs are written to logDir/zion-node-YYYY-MM-DD.log.
// Returns a closer function to flush/close the file.
func SetupFileLogging(logger *logrus.Logger, logDir string) (io.Closer, error) {
	if err := os.MkdirAll(logDir, 0750); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", logDir, err)
	}

	filename := fmt.Sprintf("zion-node-%s.log", time.Now().Format("2006-01-02"))
	logPath := filepath.Join(logDir, filename)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", logPath, err)
	}

	// Add file hook so logs go to file regardless of logger.SetOutput
	logger.AddHook(&fileHook{
		writer:    f,
		formatter: &logrus.TextFormatter{FullTimestamp: true, DisableColors: true},
	})

	return f, nil
}

// fileHook writes every log entry to a file.
type fileHook struct {
	writer    io.Writer
	formatter logrus.Formatter
}

func (h *fileHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *fileHook) Fire(entry *logrus.Entry) error {
	line, err := h.formatter.Format(entry)
	if err != nil {
		return err
	}
	_, err = h.writer.Write(line)
	return err
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
