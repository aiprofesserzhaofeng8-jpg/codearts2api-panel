package store

import "codearts2api/internal/provider"

// Legacy name retained for the existing settings API and state files.
type WorkBuddySettings = ProviderSettings

func DefaultWorkBuddySettings() WorkBuddySettings { return DefaultProviderSettings(provider.WorkBuddy) }
