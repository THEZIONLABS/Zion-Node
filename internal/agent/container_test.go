package agent

import (
	"testing"

	dockertypes "github.com/docker/docker/api/types"
)

// TestCalculateCPUPercent tests the CPU percentage calculation helper
func TestCalculateCPUPercent(t *testing.T) {
	tests := []struct {
		name           string
		totalUsage     uint64
		systemUsage    uint64
		expectedResult float64
		description    string
	}{
		{
			name:           "normal usage",
			totalUsage:     500000000,
			systemUsage:    1000000000,
			expectedResult: 50.0,
			description:    "50% CPU usage",
		},
		{
			name:           "zero total usage",
			totalUsage:     0,
			systemUsage:    1000000000,
			expectedResult: 0.0,
			description:    "no CPU usage returns 0",
		},
		{
			name:           "zero system usage",
			totalUsage:     500000000,
			systemUsage:    0,
			expectedResult: 0.0,
			description:    "no system CPU returns 0",
		},
		{
			name:           "both zero",
			totalUsage:     0,
			systemUsage:    0,
			expectedResult: 0.0,
			description:    "all zeros returns 0",
		},
		{
			name:           "full usage",
			totalUsage:     1000000000,
			systemUsage:    1000000000,
			expectedResult: 100.0,
			description:    "100% CPU usage",
		},
		{
			name:           "tiny usage",
			totalUsage:     1,
			systemUsage:    1000000000,
			expectedResult: 0.0, // Very close to 0
			description:    "negligible usage",
		},
		{
			name:           "large values",
			totalUsage:     1000000000000,
			systemUsage:    4000000000000,
			expectedResult: 25.0,
			description:    "large values - 25% usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &dockertypes.StatsJSON{}
			stats.CPUStats.CPUUsage.TotalUsage = tt.totalUsage
			stats.CPUStats.SystemUsage = tt.systemUsage

			result := calculateCPUPercent(stats)

			// Allow small floating point tolerance
			diff := result - tt.expectedResult
			if diff < -0.01 || diff > 0.01 {
				t.Errorf("calculateCPUPercent() = %f, want %f (tolerance ±0.01): %s",
					result, tt.expectedResult, tt.description)
			}
		})
	}
}

// TestCalculateCPUPercent_NilStats verifies calculateCPUPercent handles empty stats
func TestCalculateCPUPercent_EmptyStats(t *testing.T) {
	stats := &dockertypes.StatsJSON{}
	result := calculateCPUPercent(stats)
	if result != 0.0 {
		t.Errorf("Expected 0.0 for empty stats, got %f", result)
	}
}
