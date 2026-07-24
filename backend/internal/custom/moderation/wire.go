package moderation

import "github.com/google/wire"

// ProviderSet contains the custom moderation providers.
var ProviderSet = wire.NewSet(NewViolationCounter)
