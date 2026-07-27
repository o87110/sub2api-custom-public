package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type channelMonitorGroupRateRepoStub struct {
	ChannelMonitorRepository
	stored *ChannelMonitor
}

func (r *channelMonitorGroupRateRepoStub) Create(_ context.Context, monitor *ChannelMonitor) error {
	stored := *monitor
	stored.ID = 101
	r.stored = &stored
	monitor.ID = stored.ID
	return nil
}

func (r *channelMonitorGroupRateRepoStub) GetByID(_ context.Context, id int64) (*ChannelMonitor, error) {
	if r.stored == nil || r.stored.ID != id {
		return nil, ErrChannelMonitorNotFound
	}
	monitor := *r.stored
	return &monitor, nil
}

func (r *channelMonitorGroupRateRepoStub) Update(_ context.Context, monitor *ChannelMonitor) error {
	stored := *monitor
	r.stored = &stored
	return nil
}

type channelMonitorGroupRateEncryptor struct{}

func (channelMonitorGroupRateEncryptor) Encrypt(plaintext string) (string, error) {
	return "ENC:" + plaintext, nil
}

func (channelMonitorGroupRateEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "ENC:"), nil
}

func TestChannelMonitorCreateUpdateAndClearGroupRateDisplay(t *testing.T) {
	repo := &channelMonitorGroupRateRepoStub{}
	monitorService := NewChannelMonitorService(repo, channelMonitorGroupRateEncryptor{})
	override := 0.9

	created, err := monitorService.Create(context.Background(), ChannelMonitorCreateParams{
		Name:                     "group rate display",
		Provider:                 MonitorProviderOpenAI,
		Endpoint:                 "https://1.1.1.1",
		APIKey:                   "secret",
		PrimaryModel:             "gpt-test",
		GroupName:                "custom label",
		GroupRateOverride:        &override,
		GroupRateDisplayTemplate: " {rate}优先用 ",
		Enabled:                  true,
		IntervalSeconds:          60,
	})
	require.NoError(t, err)
	require.Equal(t, int64(101), created.ID)
	require.Equal(t, "custom label", created.GroupName)
	require.Equal(t, 0.9, *created.GroupRateOverride)
	require.Equal(t, "{rate}优先用", created.GroupRateDisplayTemplate)
	require.Equal(t, 0.9, *repo.stored.GroupRateOverride)

	emptyTemplate := ""
	updated, err := monitorService.Update(context.Background(), created.ID, ChannelMonitorUpdateParams{
		ClearGroupRateOverride:   true,
		GroupRateDisplayTemplate: &emptyTemplate,
	})
	require.NoError(t, err)
	require.Nil(t, updated.GroupRateOverride)
	require.Empty(t, updated.GroupRateDisplayTemplate)
	require.Nil(t, repo.stored.GroupRateOverride)

	replacement := 0.7
	customTemplate := "约{rate}x"
	updated, err = monitorService.Update(context.Background(), created.ID, ChannelMonitorUpdateParams{
		GroupRateOverride:        &replacement,
		GroupRateDisplayTemplate: &customTemplate,
	})
	require.NoError(t, err)
	require.Equal(t, 0.7, *updated.GroupRateOverride)
	require.Equal(t, customTemplate, updated.GroupRateDisplayTemplate)
}
