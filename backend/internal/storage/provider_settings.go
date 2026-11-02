package storage

import "codearts2api/internal/config"

type ProviderSettings = config.ProviderSettings

func DefaultProviderSettings(name string) ProviderSettings {
	return config.DefaultProviderSettings(name)
}
