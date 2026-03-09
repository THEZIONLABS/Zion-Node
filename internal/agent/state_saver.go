package agent

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// StateSaver handles async state persistence with batching and retry
type StateSaver struct {
	stateManager *StateManager
	saveChan     chan struct{}
	mu           sync.Mutex
	pending      bool
	logger       *logrus.Logger
	maxRetries   int
	retryDelay   time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewStateSaver creates a new state saver
func NewStateSaver(stateManager *StateManager, logger *logrus.Logger) *StateSaver {
	ctx, cancel := context.WithCancel(context.Background())
	saver := &StateSaver{
		stateManager: stateManager,
		saveChan:     make(chan struct{}, 1),
		logger:       logger,
		maxRetries:   3,
		retryDelay:   time.Second,
		ctx:          ctx,
		cancel:       cancel,
	}
	go saver.saveLoop()
	return saver
}

// Shutdown gracefully shuts down the state saver
func (s *StateSaver) Shutdown() {
	s.cancel()
	// Wait for current save to complete (with timeout)
	select {
	case <-s.ctx.Done():
	case <-time.After(5 * time.Second):
	}
}

// TriggerSave triggers a save operation (debounced)
func (s *StateSaver) TriggerSave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pending {
		s.pending = true
		select {
		case s.saveChan <- struct{}{}:
		default:
		}
	}
}

func (s *StateSaver) saveLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.saveChan:
			s.mu.Lock()
			s.pending = false
			s.mu.Unlock()

			// Retry logic
			var err error
			for attempt := 0; attempt <= s.maxRetries; attempt++ {
				// Check context before each retry
				if s.ctx.Err() != nil {
					return
				}

				err = s.stateManager.Save()
				if err == nil {
					break // Success — continue waiting for next save trigger
				}

				if attempt < s.maxRetries {
					s.logger.WithFields(logrus.Fields{
						"attempt":     attempt + 1,
						"max_retries": s.maxRetries,
						"error":       err,
					}).Warn("Failed to save state, retrying...")
					
					// Use context-aware sleep
					select {
					case <-s.ctx.Done():
						return
					case <-time.After(s.retryDelay * time.Duration(attempt+1)):
						// Continue retry
					}
				}
			}

			// All retries failed
			if err != nil {
				s.logger.WithFields(logrus.Fields{
					"max_retries": s.maxRetries,
					"error":       err,
				}).Error("Failed to save state after all retries")
			}
		}
	}
}
