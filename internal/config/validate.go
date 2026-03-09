package config

import (
	"github.com/sirupsen/logrus"
	"github.com/zion-protocol/zion-node/internal/logger"
)

// Validate validates configuration (backward compatibility)
func (c *Config) Validate() error {
	log := logger.NewLogrusLogger("info")
	return c.ValidateWithLogger(log)
}

// ValidateWithLogger validates configuration with logger
func (c *Config) ValidateWithLogger(logger *logrus.Logger) error {
	return validateConfig(c, logger)
}
