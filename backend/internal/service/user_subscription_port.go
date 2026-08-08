package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type UserSubscriptionRepository interface {
	Create(ctx context.Context, sub *UserSubscription) error
	GetByID(ctx context.Context, id int64) (*UserSubscription, error)
	GetByIDForUpdate(ctx context.Context, id int64) (*UserSubscription, error)
	GetByIDIncludeDeleted(ctx context.Context, id int64) (*UserSubscription, error)
	GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error)
	Update(ctx context.Context, sub *UserSubscription) error
	Delete(ctx context.Context, id int64) error
	Restore(ctx context.Context, subscriptionID int64, restoredStatus string) (*UserSubscription, error)

	ListByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListActiveByUserID(ctx context.Context, userID int64) ([]UserSubscription, error)
	ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error)
	List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]UserSubscription, *pagination.PaginationResult, error)

	ExistsByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExistsActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (bool, error)
	ExtendExpiry(ctx context.Context, subscriptionID int64, newExpiresAt time.Time) error
	UpdateStatus(ctx context.Context, subscriptionID int64, status string) error
	UpdateNotes(ctx context.Context, subscriptionID int64, notes string) error

	// ActivateWindows 首次使用时激活用量窗口。日窗口按日历日对齐，锚点为当天 0 点
	// （dailyStart）；周/月窗口为期限对齐滚动窗口，锚点为激活时刻（periodicStart）。
	// 仅当三个窗口均未激活时生效。
	ActivateWindows(ctx context.Context, id int64, dailyStart, periodicStart time.Time) error
	// ResetUsageWindows 手动重置所选窗口的用量。日窗口锚点写入 dailyStart（当天 0 点，
	// 保持 0 点刷新节奏不漂移）；周/月窗口锚点写入 periodicStart（重置时刻）。
	ResetUsageWindows(ctx context.Context, id int64, resetDaily, resetWeekly, resetMonthly bool, dailyStart, periodicStart time.Time) error
	ResetDailyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	ResetWeeklyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	ResetMonthlyUsage(ctx context.Context, id int64, expectedWindowStart *time.Time, newWindowStart time.Time) error
	IncrementUsage(ctx context.Context, id int64, costUSD float64) error

	BatchUpdateExpiredStatus(ctx context.Context) (int64, error)
}

// BaseUserSubscriptionRepository is the upstream persistence implementation
// before Custom cycle accounting is applied. It is a distinct Wire dependency
// so the Custom decorator can expose the final UserSubscriptionRepository.
type BaseUserSubscriptionRepository interface {
	UserSubscriptionRepository
	ListExpired(ctx context.Context) ([]UserSubscription, error)
	CountByGroupID(ctx context.Context, groupID int64) (int64, error)
	CountActiveByGroupID(ctx context.Context, groupID int64) (int64, error)
	DeleteByGroupID(ctx context.Context, groupID int64) (int64, error)
}

// UserSubscriptionCustomRepository is the narrow application port implemented
// under internal/custom. Upstream service code delegates Custom cycle, refund,
// and quota-reset orchestration through this interface.
type UserSubscriptionCustomRepository interface {
	RenewExistingTerm(ctx context.Context, subscriptionID int64, validityDays int, notes, sourceType string, sourceRef *string, assignmentSemantics bool, now, maxExpiresAt time.Time) error
	AdjustTerm(ctx context.Context, subscriptionID int64, days int, captureSnapshot, revokeIfExpired bool, now, maxExpiresAt time.Time, maxValidityDays int) (*UserSubscription, *subscriptionquota.TermSnapshot, error)
	RestoreTermSnapshotExact(ctx context.Context, snapshot *subscriptionquota.TermSnapshot) (*UserSubscription, error)
	ResetQuota(ctx context.Context, subscriptionID int64, resetDaily, resetWeekly, resetMonthly bool, operation *idempotencyexecution.Execution, now time.Time) (*UserSubscription, error)
	EnsureWindowMaintenance(ctx context.Context, sub *UserSubscription, now time.Time) (*UserSubscription, error)
}
