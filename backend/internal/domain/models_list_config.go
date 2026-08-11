package domain

// GroupModelsListConfig controls the optional custom /v1/models response list
// and the group-level model call blocklist.
type GroupModelsListConfig struct {
	Enabled       bool     `json:"enabled"`
	Models        []string `json:"models,omitempty"`
	BlockedModels []string `json:"blocked_models,omitempty"`
}
