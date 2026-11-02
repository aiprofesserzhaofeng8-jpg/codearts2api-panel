// Package scheduler contains deterministic selection rules only. Its callbacks
// borrow current state from one caller-owned critical section; no locks, clocks,
// credentials, persistence or candidate copies are owned here.
package scheduler

// Select returns the chosen index and the next cursor. Ties in least_inflight
// retain rotation order; sequential_exhaust keeps the chosen cursor. A failed
// selection leaves the cursor unchanged. Sticky selection precedes this rule.
func Select(n, cursor int, strategy string, candidate func(int) (eligible bool, inFlight int)) (int, int) {
	chosen, load := -1, 0
	for i := 0; i < n; i++ {
		idx := (cursor + i) % n
		eligible, inFlight := candidate(idx)
		if !eligible {
			continue
		}
		if chosen < 0 || strategy == "least_inflight" && inFlight < load {
			chosen, load = idx, inFlight
		}
		if strategy != "least_inflight" {
			break
		}
	}
	if chosen < 0 {
		return -1, cursor
	}
	if strategy == "sequential_exhaust" {
		return chosen, chosen
	}
	return chosen, (chosen + 1) % n
}

// Legacy selects for the existing welfare/catalog fallback, whose fail-count
// behavior deliberately differs from inference admission. Never use it to
// bypass an inference lease, group permission or cooldown.
func Legacy(n, cursor int, candidate func(int) (eligible bool, failures int)) (int, int) {
	if n == 0 {
		return -1, cursor
	}
	best, failures := -1, 0
	for i := 0; i < n; i++ {
		idx := (cursor + i) % n
		eligible, count := candidate(idx)
		if !eligible {
			continue
		}
		if best < 0 || count < failures {
			best, failures = idx, count
		}
		if count == 0 {
			return idx, (idx + 1) % n
		}
	}
	return best, (cursor + 1) % n
}

func MaxInFlight(override, fallback int) int {
	if override >= 1 && override <= 32 {
		return override
	}
	return fallback
}
