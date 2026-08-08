package subscriptionquota

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/idempotencyrecord"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
)

// ClaimResetOperation records an item-level reset claim in the same database
// transaction as the quota reset. This closes the crash window between batch
// side effects and the outer HTTP idempotency result.
func ClaimResetOperation(ctx context.Context, client *dbent.Client, subscriptionID int64, operationKeyHash string, claimedAt, expiresAt time.Time) (bool, error) {
	operationKeyHash = strings.TrimSpace(operationKeyHash)
	if operationKeyHash == "" {
		return true, nil
	}
	if claimedAt.IsZero() || !expiresAt.After(claimedAt) {
		return false, fmt.Errorf("record subscription quota reset operation: invalid operation lifetime")
	}
	if _, err := client.UserSubscription.Query().
		Where(usersubscription.IDEQ(subscriptionID)).
		ForUpdate().
		Only(ctx); err != nil {
		return false, err
	}
	scope := "custom.subscription_quota_reset." + strconv.FormatInt(subscriptionID, 10)
	existing, err := client.IdempotencyRecord.Query().
		Where(
			idempotencyrecord.ScopeEQ(scope),
			idempotencyrecord.IdempotencyKeyHashEQ(operationKeyHash),
		).
		ForUpdate().
		Only(ctx)
	if err != nil && !dbent.IsNotFound(err) {
		return false, err
	}
	if err == nil {
		if existing.ExpiresAt.After(claimedAt) {
			if !existing.ExpiresAt.Equal(expiresAt) {
				if _, err := existing.Update().SetExpiresAt(expiresAt).Save(ctx); err != nil {
					return false, fmt.Errorf("extend subscription quota reset operation: %w", err)
				}
			}
			return false, nil
		}
		// Reclaim by replacing the expired row while the subscription lock is
		// held. A new row ID prevents a concurrent expiry cleanup from deleting
		// the reclaimed claim after this transaction commits.
		if err := client.IdempotencyRecord.DeleteOne(existing).Exec(ctx); err != nil && !dbent.IsNotFound(err) {
			return false, fmt.Errorf("delete expired subscription quota reset operation: %w", err)
		}
	}
	if _, err := client.IdempotencyRecord.Create().
		SetScope(scope).
		SetIdempotencyKeyHash(operationKeyHash).
		SetRequestFingerprint(operationKeyHash).
		SetStatus("succeeded").
		SetResponseStatus(200).
		SetResponseBody("{}").
		SetExpiresAt(expiresAt).
		Save(ctx); err != nil {
		return false, fmt.Errorf("record subscription quota reset operation: %w", err)
	}
	return true, nil
}
