package channelmonitor

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type groupRateMonitorListerStub struct {
	monitors []*service.ChannelMonitor
	err      error
}

func (s *groupRateMonitorListerStub) ListEnabledMonitors(context.Context) ([]*service.ChannelMonitor, error) {
	return s.monitors, s.err
}

type groupRateLookupStub struct {
	rates     map[string]GroupRate
	err       error
	keys      []string
	callCount int
}

func (s *groupRateLookupStub) ListByAPIKeys(_ context.Context, keys []string) (map[string]GroupRate, error) {
	s.callCount++
	s.keys = append([]string(nil), keys...)
	return s.rates, s.err
}

func TestGroupRateResolverResolve(t *testing.T) {
	lookup := &groupRateLookupStub{
		rates: map[string]GroupRate{
			"sk-local-openai": {Platform: "openai", RateMultiplier: 0.18},
			"sk-zero":         {Platform: "openai", RateMultiplier: 0},
			"sk-mismatch":     {Platform: "anthropic", RateMultiplier: 2},
		},
	}
	resolver := newGroupRateResolver(
		&groupRateMonitorListerStub{monitors: []*service.ChannelMonitor{
			{ID: 1, Provider: "openai", APIKey: "sk-local-openai"},
			{ID: 2, Provider: "openai", APIKey: "sk-zero"},
			{ID: 3, Provider: "openai", APIKey: "sk-mismatch"},
			{ID: 4, Provider: "openai", APIKey: "sk-external"},
			{ID: 5, Provider: "openai", APIKeyDecryptFailed: true},
			{ID: 6, Provider: "openai", APIKey: "sk-local-openai"},
			nil,
		}},
		lookup,
	)

	got := resolver.Resolve(context.Background())

	require.Equal(t, map[int64]float64{
		1: 0.18,
		2: 0,
		6: 0.18,
	}, got)
	require.Equal(t, 1, lookup.callCount)
	require.ElementsMatch(t, []string{
		"sk-local-openai",
		"sk-zero",
		"sk-mismatch",
		"sk-external",
	}, lookup.keys)
}

func TestGroupRateResolverResolveRefreshesCurrentRate(t *testing.T) {
	lookup := &groupRateLookupStub{
		rates: map[string]GroupRate{
			"sk-local": {Platform: "openai", RateMultiplier: 0.18},
		},
	}
	resolver := newGroupRateResolver(
		&groupRateMonitorListerStub{monitors: []*service.ChannelMonitor{
			{ID: 1, Provider: "openai", APIKey: "sk-local"},
		}},
		lookup,
	)

	require.Equal(t, 0.18, resolver.Resolve(context.Background())[1])

	lookup.rates["sk-local"] = GroupRate{Platform: "openai", RateMultiplier: 0.2}
	require.Equal(t, 0.2, resolver.Resolve(context.Background())[1])
	require.Equal(t, 2, lookup.callCount)
}

func TestGroupRateResolverResolveFallsBackOnErrors(t *testing.T) {
	t.Run("monitor list failure", func(t *testing.T) {
		lookup := &groupRateLookupStub{}
		resolver := newGroupRateResolver(
			&groupRateMonitorListerStub{err: errors.New("list failed")},
			lookup,
		)

		require.Empty(t, resolver.Resolve(context.Background()))
		require.Zero(t, lookup.callCount)
	})

	t.Run("batch lookup failure", func(t *testing.T) {
		resolver := newGroupRateResolver(
			&groupRateMonitorListerStub{monitors: []*service.ChannelMonitor{
				{ID: 1, Provider: "openai", APIKey: "sk-local"},
			}},
			&groupRateLookupStub{err: errors.New("lookup failed")},
		)

		require.Empty(t, resolver.Resolve(context.Background()))
	})
}
