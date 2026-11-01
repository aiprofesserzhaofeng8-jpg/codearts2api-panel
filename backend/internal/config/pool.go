package config

import "fmt"

// PoolSettings governs inference scheduling. Background account maintenance remains independent.
type PoolSettings struct {
	Strategy           string `json:"strategy"`
	MaxInFlight        int    `json:"max_in_flight"`
	StickyTTLSec       int    `json:"sticky_ttl_sec"`
	RateCooldownSec    int    `json:"rate_cooldown_sec"`
	BreakerThreshold   int    `json:"breaker_threshold"`
	BreakerCooldownSec int    `json:"breaker_cooldown_sec"`
}

func DefaultPoolSettings() PoolSettings {
	return PoolSettings{Strategy: "least_inflight", MaxInFlight: 2, StickyTTLSec: 1800,
		RateCooldownSec: 60, BreakerThreshold: 3, BreakerCooldownSec: 300}
}

func (p PoolSettings) Validate() error {
	switch p.Strategy {
	case "round_robin", "least_inflight", "sequential_exhaust":
	default:
		return fmt.Errorf("pool.strategy 必须为 round_robin / least_inflight / sequential_exhaust")
	}
	for _, v := range []struct {
		name            string
		value, min, max int
	}{
		{"max_in_flight", p.MaxInFlight, 1, 32}, {"sticky_ttl_sec", p.StickyTTLSec, 0, 86400},
		{"rate_cooldown_sec", p.RateCooldownSec, 0, 86400}, {"breaker_threshold", p.BreakerThreshold, 1, 100},
		{"breaker_cooldown_sec", p.BreakerCooldownSec, 1, 86400},
	} {
		if v.value < v.min || v.value > v.max {
			return fmt.Errorf("pool.%s 必须在 %d–%d 之间", v.name, v.min, v.max)
		}
	}
	return nil
}
