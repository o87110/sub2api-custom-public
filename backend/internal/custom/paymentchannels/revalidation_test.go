package paymentchannels

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectionMatchesRequiresStableEnabledRevision(t *testing.T) {
	record := RevisionRecord{ProviderKey: ProviderEasyPay, Config: "{}", SupportedTypes: "alipay", Enabled: true}
	revision := InstanceRevision(record)
	require.True(t, SelectionMatches(record, SelectionSnapshot{ProviderKey: ProviderEasyPay, Revision: revision}, true))

	changed := record
	changed.Config = `{"merchant":"changed"}`
	require.False(t, SelectionMatches(changed, SelectionSnapshot{ProviderKey: ProviderEasyPay, Revision: revision}, true))
	require.False(t, SelectionMatches(record, SelectionSnapshot{ProviderKey: ProviderEasyPay, Revision: revision}, false))
}
