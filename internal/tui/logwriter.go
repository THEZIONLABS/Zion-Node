package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LogEntry is a single captured log line.
type LogEntry struct {
	Time    time.Time
	Level   string // "DEBUG", "INFO", "WARN", "ERROR"
	Module  string // extracted from logrus field "module" (may be empty)
	Message string
}

// String renders the entry for display.
func (e LogEntry) String() string {
	ts := e.Time.Format("15:04:05")
	lvl := fmt.Sprintf("%-5s", strings.ToUpper(e.Level))
	if e.Module != "" {
		return fmt.Sprintf("%s  %s  %-10s %s", ts, lvl, e.Module, e.Message)
	}
	return fmt.Sprintf("%s  %s  %s", ts, lvl, e.Message)
}

// LogBuffer is a thread-safe ring-buffer that implements logrus.Hook.
// TUI reads from it; logrus writes to it.
type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	maxSize int
	ch      chan struct{} // non-blocking notification channel
}

// NewLogBuffer creates a buffer with the given capacity.
func NewLogBuffer(size int) *LogBuffer {
	if size <= 0 {
		size = 500
	}
	return &LogBuffer{
		entries: make([]LogEntry, 0, size),
		maxSize: size,
		ch:      make(chan struct{}, 1),
	}
}

// Levels implements logrus.Hook — capture all levels.
func (b *LogBuffer) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire implements logrus.Hook — called by logrus on every log.
func (b *LogBuffer) Fire(entry *logrus.Entry) error {
	module := ""
	if m, ok := entry.Data["module"]; ok {
		module = fmt.Sprintf("%v", m)
	}

	// Build a clean message that includes key fields from logrus Data.
	msg := entry.Message
	if len(entry.Data) > 0 {
		var parts []string
		for k, v := range entry.Data {
			if k == "module" {
				continue
			}
			s := fmt.Sprintf("%v", v)
			if len(s) > 40 {
				s = s[:37] + "..."
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, s))
		}
		if len(parts) > 0 {
			msg = msg + "  " + strings.Join(parts, " ")
		}
	}

	le := LogEntry{
		Time:    entry.Time,
		Level:   entry.Level.String(),
		Module:  module,
		Message: msg,
	}

	b.mu.Lock()
	if len(b.entries) >= b.maxSize {
		// shift left by 1 (drop oldest)
		copy(b.entries, b.entries[1:])
		b.entries[len(b.entries)-1] = le
	} else {
		b.entries = append(b.entries, le)
	}
	b.mu.Unlock()

	// non-blocking notification
	select {
	case b.ch <- struct{}{}:
	default:
	}
	return nil
}

// Entries returns a snapshot, optionally filtered by level.
// Pass "" or "ALL" for no filtering.
func (b *LogBuffer) Entries(level string) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	level = strings.ToUpper(level)
	if level == "" || level == "ALL" {
		out := make([]LogEntry, len(b.entries))
		copy(out, b.entries)
		return out
	}

	var out []LogEntry
	for _, e := range b.entries {
		if strings.EqualFold(e.Level, level) {
			out = append(out, e)
		}
	}
	return out
}

// Len returns the current entry count.
func (b *LogBuffer) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.entries)
}

// Notify returns the notification channel.
// Consumers should select on this in a tea.Cmd to know when new logs arrive.
func (b *LogBuffer) Notify() <-chan struct{} {
	return b.ch
}
