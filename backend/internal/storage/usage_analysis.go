package storage

import (
	"time"

	"codearts2api/internal/usage"
)

type UsageDistribution = usage.UsageDistribution
type UsageTrendPoint = usage.UsageTrendPoint
type UsageAnalysis = usage.UsageAnalysis

func analyzeUsage(records []LogEntry, location *time.Location) UsageAnalysis {
	return usage.Analyze(records, location)
}
