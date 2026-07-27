package channelmonitor

import "github.com/google/wire"

// ProviderSet contains the custom channel monitor group-rate providers.
var ProviderSet = wire.NewSet(
	NewEntGroupRateLookup,
	NewGroupRateResolver,
)
