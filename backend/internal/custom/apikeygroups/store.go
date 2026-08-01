package apikeygroups

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/apikeygroup"
	"github.com/Wei-Shaw/sub2api/ent/group"

	entsql "entgo.io/ent/dialect/sql"
)

type APIKeyCreateParams struct {
	UserID      int64
	Key         string
	Name        string
	Status      string
	GroupID     *int64
	IPWhitelist []string
	IPBlacklist []string
	LastUsedAt  *time.Time
	Quota       float64
	QuotaUsed   float64
	ExpiresAt   *time.Time
	RateLimit5h float64
	RateLimit1d float64
	RateLimit7d float64
}

// PersistAPIKeyWithGroups creates an API key and its ordered group assignments
// atomically. The caller remains responsible for translating persistence errors
// into the public service error contract.
func PersistAPIKeyWithGroups(
	ctx context.Context,
	fallback *dbent.Client,
	key APIKeyCreateParams,
	groupIDs []int64,
) (*dbent.APIKey, error) {
	var created *dbent.APIKey
	err := withTransaction(ctx, fallback, func(txCtx context.Context, client *dbent.Client) error {
		builder := client.APIKey.Create().
			SetUserID(key.UserID).
			SetKey(key.Key).
			SetName(key.Name).
			SetStatus(key.Status).
			SetNillableGroupID(key.GroupID).
			SetNillableLastUsedAt(key.LastUsedAt).
			SetQuota(key.Quota).
			SetQuotaUsed(key.QuotaUsed).
			SetNillableExpiresAt(key.ExpiresAt).
			SetRateLimit5h(key.RateLimit5h).
			SetRateLimit1d(key.RateLimit1d).
			SetRateLimit7d(key.RateLimit7d)
		if len(key.IPWhitelist) > 0 {
			builder.SetIPWhitelist(key.IPWhitelist)
		}
		if len(key.IPBlacklist) > 0 {
			builder.SetIPBlacklist(key.IPBlacklist)
		}

		var err error
		created, err = builder.Save(txCtx)
		if err != nil {
			return err
		}
		return createAssignments(txCtx, client, created.ID, groupIDs)
	})
	return created, err
}

// LoadOrderedGroups returns assignments ordered by API key and priority, with
// each referenced group hydrated.
func LoadOrderedGroups(
	ctx context.Context,
	fallback *dbent.Client,
	apiKeyIDs []int64,
) ([]*dbent.APIKeyGroup, error) {
	if len(apiKeyIDs) == 0 {
		return nil, nil
	}
	return clientFromContext(ctx, fallback).APIKeyGroup.Query().
		Where(apikeygroup.APIKeyIDIn(apiKeyIDs...)).
		WithGroup().
		Order(apikeygroup.ByAPIKeyID(), apikeygroup.ByPriority()).
		All(ctx)
}

// ReplaceGroups atomically replaces an API key's ordered assignments and
// synchronizes the legacy api_keys.group_id alias.
func ReplaceGroups(
	ctx context.Context,
	fallback *dbent.Client,
	apiKeyID int64,
	groupIDs []int64,
) (int, error) {
	var affected int
	err := withTransaction(ctx, fallback, func(txCtx context.Context, client *dbent.Client) error {
		var err error
		affected, err = replaceGroups(txCtx, client, apiKeyID, groupIDs)
		return err
	})
	return affected, err
}

// RemoveGroupAssignments removes one group from every API key, compacts the
// remaining priorities, and promotes the next assignment. The supplied client
// is expected to belong to the caller's group-deletion transaction.
func RemoveGroupAssignments(
	ctx context.Context,
	client *dbent.Client,
	groupID int64,
) ([]int64, error) {
	links, err := client.APIKeyGroup.Query().
		Where(apikeygroup.GroupIDEQ(groupID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	apiKeyIDs := make([]int64, 0, len(links))
	seen := make(map[int64]struct{}, len(links))
	for _, link := range links {
		if _, ok := seen[link.APIKeyID]; ok {
			continue
		}
		seen[link.APIKeyID] = struct{}{}
		apiKeyIDs = append(apiKeyIDs, link.APIKeyID)
	}
	for _, apiKeyID := range apiKeyIDs {
		assignments, queryErr := client.APIKeyGroup.Query().
			Where(apikeygroup.APIKeyIDEQ(apiKeyID)).
			Order(apikeygroup.ByPriority()).
			All(ctx)
		if queryErr != nil {
			return nil, queryErr
		}
		remaining := make([]int64, 0, len(assignments))
		for _, assignment := range assignments {
			if assignment.GroupID != groupID {
				remaining = append(remaining, assignment.GroupID)
			}
		}
		if _, replaceErr := replaceGroups(ctx, client, apiKeyID, remaining); replaceErr != nil {
			return nil, replaceErr
		}
	}
	return apiKeyIDs, nil
}

// ListAPIKeysByAssignedGroup returns API keys whose ordered list contains the
// requested group at any priority.
func ListAPIKeysByAssignedGroup(
	ctx context.Context,
	fallback *dbent.Client,
	groupID int64,
	offset int,
	limit int,
	orders []func(*entsql.Selector),
) ([]*dbent.APIKey, int, error) {
	query := clientFromContext(ctx, fallback).APIKey.Query().
		Where(apikey.DeletedAtIsNil(), apikey.HasGroupsWith(group.IDEQ(groupID)))
	total, err := query.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	keysQuery := query.WithUser().Offset(offset).Limit(limit)
	for _, order := range orders {
		keysQuery = keysQuery.Order(order)
	}
	keys, err := keysQuery.All(ctx)
	return keys, total, err
}

// CountAPIKeysByAssignedGroup counts API keys containing the group at any
// priority.
func CountAPIKeysByAssignedGroup(
	ctx context.Context,
	fallback *dbent.Client,
	groupID int64,
) (int64, error) {
	count, err := clientFromContext(ctx, fallback).APIKey.Query().
		Where(apikey.DeletedAtIsNil(), apikey.HasGroupsWith(group.IDEQ(groupID))).
		Count(ctx)
	return int64(count), err
}

// ListKeysByAssignedGroup returns credential values for auth-cache
// invalidation. It never logs or persists the returned values.
func ListKeysByAssignedGroup(
	ctx context.Context,
	fallback *dbent.Client,
	groupID int64,
) ([]string, error) {
	return clientFromContext(ctx, fallback).APIKey.Query().
		Where(apikey.DeletedAtIsNil(), apikey.HasGroupsWith(group.IDEQ(groupID))).
		Select(apikey.FieldKey).
		Strings(ctx)
}

// HasPlatformConflict reports whether changing a group platform would create
// a mixed-platform ordered list.
func HasPlatformConflict(
	ctx context.Context,
	fallback *dbent.Client,
	groupID int64,
	platform string,
) (bool, error) {
	return clientFromContext(ctx, fallback).APIKeyGroup.Query().
		Where(
			apikeygroup.GroupIDEQ(groupID),
			apikeygroup.HasAPIKeyWith(
				apikey.DeletedAtIsNil(),
				apikey.HasGroupsWith(group.IDNEQ(groupID), group.PlatformNEQ(platform)),
			),
		).
		Exist(ctx)
}

// ReplaceUserGroup replaces one group in every matching API key owned by the
// user, preserving order and removing duplicates.
func ReplaceUserGroup(
	ctx context.Context,
	fallback *dbent.Client,
	userID int64,
	oldGroupID int64,
	newGroupID int64,
) (int64, error) {
	var migrated int64
	err := withTransaction(ctx, fallback, func(txCtx context.Context, client *dbent.Client) error {
		keys, err := client.APIKey.Query().
			Where(
				apikey.UserIDEQ(userID),
				apikey.DeletedAtIsNil(),
				apikey.Or(
					apikey.GroupIDEQ(oldGroupID),
					apikey.HasGroupsWith(group.IDEQ(oldGroupID)),
				),
			).
			All(txCtx)
		if err != nil {
			return err
		}
		for _, key := range keys {
			assignments, queryErr := client.APIKeyGroup.Query().
				Where(apikeygroup.APIKeyIDEQ(key.ID)).
				Order(apikeygroup.ByPriority()).
				All(txCtx)
			if queryErr != nil {
				return queryErr
			}
			current := make([]int64, 0, len(assignments))
			if len(assignments) == 0 && key.GroupID != nil {
				current = append(current, *key.GroupID)
			} else {
				for _, assignment := range assignments {
					current = append(current, assignment.GroupID)
				}
			}
			replaced := make([]int64, 0, len(current))
			seen := make(map[int64]struct{}, len(current))
			for _, candidate := range current {
				if candidate == oldGroupID {
					candidate = newGroupID
				}
				if _, duplicate := seen[candidate]; duplicate {
					continue
				}
				seen[candidate] = struct{}{}
				replaced = append(replaced, candidate)
			}
			if validateErr := validateOnePlatform(txCtx, client, replaced); validateErr != nil {
				return validateErr
			}
			if _, replaceErr := replaceGroups(txCtx, client, key.ID, replaced); replaceErr != nil {
				return replaceErr
			}
		}
		migrated = int64(len(keys))
		return nil
	})
	return migrated, err
}

func validateOnePlatform(ctx context.Context, client *dbent.Client, groupIDs []int64) error {
	if len(groupIDs) < 2 {
		return nil
	}
	groups, err := client.Group.Query().
		Where(group.IDIn(groupIDs...)).
		Select(group.FieldID, group.FieldPlatform).
		All(ctx)
	if err != nil {
		return err
	}
	if len(groups) != len(groupIDs) {
		return ErrInvalidSelection
	}
	platform := groups[0].Platform
	for i := 1; i < len(groups); i++ {
		if groups[i].Platform != platform {
			return ErrMixedPlatforms
		}
	}
	return nil
}

func replaceGroups(
	ctx context.Context,
	client *dbent.Client,
	apiKeyID int64,
	groupIDs []int64,
) (int, error) {
	if _, err := client.APIKeyGroup.Delete().
		Where(apikeygroup.APIKeyIDEQ(apiKeyID)).
		Exec(ctx); err != nil {
		return 0, err
	}
	if err := createAssignments(ctx, client, apiKeyID, groupIDs); err != nil {
		return 0, err
	}
	update := client.APIKey.Update().
		Where(apikey.IDEQ(apiKeyID), apikey.DeletedAtIsNil()).
		SetUpdatedAt(time.Now())
	if len(groupIDs) > 0 {
		update.SetGroupID(groupIDs[0])
	} else {
		update.ClearGroupID()
	}
	return update.Save(ctx)
}

func createAssignments(
	ctx context.Context,
	client *dbent.Client,
	apiKeyID int64,
	groupIDs []int64,
) error {
	if len(groupIDs) == 0 {
		return nil
	}
	builders := make([]*dbent.APIKeyGroupCreate, 0, len(groupIDs))
	for priority, groupID := range groupIDs {
		builders = append(builders, client.APIKeyGroup.Create().
			SetAPIKeyID(apiKeyID).
			SetGroupID(groupID).
			SetPriority(priority))
	}
	_, err := client.APIKeyGroup.CreateBulk(builders...).Save(ctx)
	return err
}

func withTransaction(
	ctx context.Context,
	fallback *dbent.Client,
	operation func(context.Context, *dbent.Client) error,
) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return operation(ctx, tx.Client())
	}
	tx, err := fallback.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := operation(txCtx, tx.Client()); err != nil {
		return err
	}
	return tx.Commit()
}

func clientFromContext(ctx context.Context, fallback *dbent.Client) *dbent.Client {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return tx.Client()
	}
	return fallback
}
