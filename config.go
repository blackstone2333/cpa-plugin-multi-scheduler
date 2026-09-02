package main

import (
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Enabled               bool          `yaml:"enabled" json:"enabled"`
	TierToleranceHours    int           `yaml:"tier_tolerance_hours" json:"tier_tolerance_hours"`
	LowFiveHourThreshold  float64       `yaml:"low_five_hour_threshold" json:"low_five_hour_threshold"`
	BasePriority          int           `yaml:"base_priority" json:"base_priority"`
	PriorityStep          int           `yaml:"priority_step" json:"priority_step"`
	MinimumPriority       int           `yaml:"minimum_priority" json:"minimum_priority"`
	ExhaustedPriority     int           `yaml:"exhausted_priority" json:"exhausted_priority"`
	PollIntervalHighMin   int           `yaml:"poll_interval_high_min" json:"poll_interval_high_min"`
	PollIntervalMediumMin int           `yaml:"poll_interval_medium_min" json:"poll_interval_medium_min"`
	PollIntervalLowMin    int           `yaml:"poll_interval_low_min" json:"poll_interval_low_min"`
	StaggerIntervalSec    int           `yaml:"stagger_interval_sec" json:"stagger_interval_sec"`
	SyncToHostPriority    bool          `yaml:"sync_to_host_priority" json:"sync_to_host_priority"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:               true,
		TierToleranceHours:    16,
		LowFiveHourThreshold:  0.05,
		BasePriority:          400,
		PriorityStep:          100,
		MinimumPriority:       100,
		ExhaustedPriority:     -1000,
		PollIntervalHighMin:   20,
		PollIntervalMediumMin: 5,
		PollIntervalLowMin:    3,
		StaggerIntervalSec:    2,
		SyncToHostPriority:    true,
	}
}

func (c Config) TierTolerance() time.Duration {
	return time.Duration(c.TierToleranceHours) * time.Hour
}

func DecodeConfig(raw []byte) (Config, error) {
	cfg := DefaultConfig()
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
