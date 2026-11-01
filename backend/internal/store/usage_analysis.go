package store

import (
	"sort"
	"time"
)

type UsageDistribution struct {
	Name  string `json:"name"`
	Stats Stats  `json:"stats"`
}
type UsageTrendPoint struct {
	Time  int64 `json:"time"`
	Stats Stats `json:"stats"`
}
type UsageAnalysis struct {
	Models     []UsageDistribution `json:"models"`
	Providers  []UsageDistribution `json:"providers"`
	Protocols  []UsageDistribution `json:"protocols"`
	Trend      []UsageTrendPoint   `json:"trend"`
	BucketDays int64               `json:"bucket_days"`
	TimeZone   string              `json:"time_zone"`
}

// Charts use the full filtered history, never the current table page. Calendar
// days follow the browser's IANA time zone, including daylight-saving changes.
// Long ranges combine adjacent calendar days into at most 90 trend buckets.
func analyzeUsage(records []LogEntry, location *time.Location) UsageAnalysis {
	if location == nil {
		location = time.UTC
	}
	a := UsageAnalysis{Models: []UsageDistribution{}, Providers: []UsageDistribution{}, Protocols: []UsageDistribution{}, Trend: []UsageTrendPoint{}, BucketDays: 1, TimeZone: location.String()}
	if len(records) == 0 {
		return a
	}
	day := func(stamp int64) int64 {
		t := time.Unix(stamp, 0).In(location)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400
	}
	first, last := day(records[0].Time), day(records[0].Time)
	for _, r := range records {
		d := day(r.Time)
		first, last = min(first, d), max(last, d)
	}
	a.BucketDays = max(1, (last-first+90)/90)
	models, providers, protocols, trend := map[string]*Stats{}, map[string]*Stats{}, map[string]*Stats{}, map[int64]*Stats{}
	add := func(target map[string]*Stats, name string, r LogEntry) {
		if name == "" {
			name = "unknown"
		}
		if target[name] == nil {
			target[name] = &Stats{}
		}
		sumUsageRecord(target[name], r)
	}
	for _, r := range records {
		name := r.Provider
		if name == "" {
			name = "codearts"
		}
		add(models, r.Model, r)
		add(providers, name, r)
		add(protocols, r.Protocol, r)
		bucket := (day(r.Time) - first) / a.BucketDays
		if trend[bucket] == nil {
			trend[bucket] = &Stats{}
		}
		sumUsageRecord(trend[bucket], r)
	}
	list := func(values map[string]*Stats) []UsageDistribution {
		out := make([]UsageDistribution, 0, len(values))
		for name, stats := range values {
			out = append(out, UsageDistribution{Name: name, Stats: *stats})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	}
	a.Models, a.Providers, a.Protocols = list(models), list(providers), list(protocols)
	for bucket := int64(0); bucket <= (last-first)/a.BucketDays; bucket++ {
		d := time.Unix((first+bucket*a.BucketDays)*86400, 0).UTC()
		point := UsageTrendPoint{Time: time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, location).Unix()}
		if stats := trend[bucket]; stats != nil {
			point.Stats = *stats
		}
		a.Trend = append(a.Trend, point)
	}
	return a
}
