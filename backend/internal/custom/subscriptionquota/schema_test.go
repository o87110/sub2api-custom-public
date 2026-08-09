package subscriptionquota

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/stretchr/testify/require"
)

func TestCycleSchemaEnforcesCurrentUniquenessAndCascadeDelete(t *testing.T) {
	ctx := context.Background()
	client := newCycleSchemaTestClient(t)
	user, err := client.User.Create().
		SetEmail("cycle-schema@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName("cycle-schema-group").Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()
	subscription, err := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetStartsAt(now).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetCurrentCycleStartsAt(now).
		SetCurrentCycleEndsAt(now.Add(24 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	cycle, err := client.UserSubscriptionCycle.Create().
		SetSubscriptionID(subscription.ID).
		SetStartsAt(now).
		SetEndsAt(now.Add(24 * time.Hour)).
		SetStatus(CycleStatusCurrent).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserSubscriptionCycle.Create().
		SetSubscriptionID(subscription.ID).
		SetStartsAt(now.Add(time.Hour)).
		SetEndsAt(now.Add(25 * time.Hour)).
		SetStatus(CycleStatusCurrent).
		Save(ctx)
	require.Error(t, err, "a subscription must not have two current cycles")

	err = client.UserSubscription.DeleteOneID(subscription.ID).Exec(mixins.SkipSoftDelete(ctx))
	require.NoError(t, err)
	_, err = client.UserSubscriptionCycle.Get(ctx, cycle.ID)
	require.True(t, dbent.IsNotFound(err), "physical subscription deletion must cascade to cycle rows")
}

func TestCycleSchemaEnforcesTraceableSourceUniqueness(t *testing.T) {
	ctx := context.Background()
	client := newCycleSchemaTestClient(t)
	user, err := client.User.Create().
		SetEmail("cycle-source-schema@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName("cycle-source-schema-group").Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()
	for index := 0; index < 2; index++ {
		subscription, createErr := client.UserSubscription.Create().
			SetUserID(user.ID).
			SetGroupID(group.ID).
			SetStartsAt(now).
			SetExpiresAt(now.Add(24 * time.Hour)).
			SetCurrentCycleStartsAt(now).
			SetCurrentCycleEndsAt(now.Add(24 * time.Hour)).
			Save(ctx)
		if index == 0 {
			require.NoError(t, createErr)
			_, createErr = client.UserSubscriptionCycle.Create().
				SetSubscriptionID(subscription.ID).
				SetStartsAt(now).
				SetEndsAt(now.Add(24 * time.Hour)).
				SetStatus(CycleStatusPending).
				SetSourceType(CycleSourcePayment).
				SetSourceRef("payment-order-42").
				Save(ctx)
			require.NoError(t, createErr)
			continue
		}
		require.NoError(t, createErr)
		_, createErr = client.UserSubscriptionCycle.Create().
			SetSubscriptionID(subscription.ID).
			SetStartsAt(now).
			SetEndsAt(now.Add(24 * time.Hour)).
			SetStatus(CycleStatusPending).
			SetSourceType(CycleSourcePayment).
			SetSourceRef("payment-order-42").
			Save(ctx)
		require.Error(t, createErr, "the same payment source must not be attributed twice")
	}
}

func TestCreateCurrentCyclePersistsManualEligibilityOnlyForAdministeredSources(t *testing.T) {
	ctx := context.Background()
	client := newCycleSchemaTestClient(t)
	user, err := client.User.Create().
		SetEmail("cycle-manual-eligibility@example.com").
		SetPasswordHash("hash").
		Save(ctx)
	require.NoError(t, err)
	now := time.Now().UTC()

	for index, testCase := range []struct {
		source string
		want   bool
	}{
		{source: CycleSourceAssignment, want: true},
		{source: CycleSourceLegacy, want: true},
		{source: CycleSourcePayment, want: false},
		{source: CycleSourceRedeem, want: false},
	} {
		group, createErr := client.Group.Create().SetName("cycle-eligibility-" + testCase.source).Save(ctx)
		require.NoError(t, createErr)
		subscription, createErr := client.UserSubscription.Create().
			SetUserID(user.ID).
			SetGroupID(group.ID).
			SetStartsAt(now).
			SetExpiresAt(now.Add(24 * time.Hour)).
			SetCurrentCycleStartsAt(now).
			SetCurrentCycleEndsAt(now.Add(24 * time.Hour)).
			Save(ctx)
		require.NoError(t, createErr, "case %d", index)
		require.NoError(t, CreateCurrentCycle(ctx, client, subscription.ID, now, now.Add(24*time.Hour), testCase.source, nil, true))
		cycle, queryErr := subscription.QueryCycles().Only(ctx)
		require.NoError(t, queryErr)
		require.Equal(t, testCase.want, cycle.ManualBulkQuotaResetEnabled)
	}
}
