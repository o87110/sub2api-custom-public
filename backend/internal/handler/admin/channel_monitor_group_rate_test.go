package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorToResponseIncludesGroupNameAndRateDisplayConfig(t *testing.T) {
	override := 0.9
	response := channelMonitorToResponse(&service.ChannelMonitor{
		GroupName:                "custom label",
		GroupRateOverride:        &override,
		GroupRateDisplayTemplate: "{rate}优先用",
	})

	require.Equal(t, "custom label", response.GroupName)
	require.Equal(t, 0.9, *response.GroupRateOverride)
	require.Equal(t, "{rate}优先用", response.GroupRateDisplayTemplate)
}
