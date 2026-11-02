// Package store is the deprecated source compatibility facade for storage.
// New production code imports storage or a consumer-owned business interface.
package store

import "codearts2api/internal/storage"

type APIKey = storage.APIKey
type AccessScope = storage.AccessScope
type AccountGroup = storage.AccountGroup
type AuditEvent = storage.AuditEvent
type BudgetError = storage.BudgetError
type BudgetUsage = storage.BudgetUsage
type KeyBudget = storage.KeyBudget
type LogEntry = storage.LogEntry
type ModelHealth = storage.ModelHealth
type ModelMapping = storage.ModelMapping
type ModelPolicy = storage.ModelPolicy
type ModelPrice = storage.ModelPrice
type PoolAccountState = storage.PoolAccountState
type PoolSettings = storage.PoolSettings
type ProviderSettings = storage.ProviderSettings
type RequestAttempt = storage.RequestAttempt
type RequestCost = storage.RequestCost
type Settings = storage.Settings
type State = storage.State
type Stats = storage.Stats
type Store = storage.Store
type TokenUsage = storage.TokenUsage
type UsageAnalysis = storage.UsageAnalysis
type UsageDistribution = storage.UsageDistribution
type UsagePage = storage.UsagePage
type UsageQuery = storage.UsageQuery
type UsageTrendPoint = storage.UsageTrendPoint
type WorkBuddySettings = storage.WorkBuddySettings

const StateVersion = storage.StateVersion
const MaxUsageRecords = storage.MaxUsageRecords

var ErrAccountExists = storage.ErrAccountExists
var ErrUsagePersistence = storage.ErrUsagePersistence

func Open(path string) (*Store, error)  { return storage.Open(path) }
func DefaultSettings() Settings         { return storage.DefaultSettings() }
func DefaultPoolSettings() PoolSettings { return storage.DefaultPoolSettings() }
func DefaultProviderSettings(name string) ProviderSettings {
	return storage.DefaultProviderSettings(name)
}
func DefaultWorkBuddySettings() WorkBuddySettings       { return storage.DefaultWorkBuddySettings() }
func DefaultModelPolicies() map[string]ModelPolicy      { return storage.DefaultModelPolicies() }
func HashAdminPassword(password string) (string, error) { return storage.HashAdminPassword(password) }
func VerifyAdminPassword(stored, password string) bool {
	return storage.VerifyAdminPassword(stored, password)
}
