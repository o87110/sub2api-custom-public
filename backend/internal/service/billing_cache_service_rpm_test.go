//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// userRPMCacheStub 记录每种计数器被调用的次数，并可注入返回值与错误。
type userRPMCacheStub struct {
	userGroupCalls int32
	userCalls      int32
	userGroupReads int32
	userReads      int32

	userGroupCounts []int // 依次返回的计数值
	userGroupErr    error
	userCounts      []int
	userErr         error
}

func (s *userRPMCacheStub) IncrementUserGroupRPM(_ context.Context, _, _ int64) (int, error) {
	idx := int(atomic.AddInt32(&s.userGroupCalls, 1)) - 1
	if s.userGroupErr != nil {
		return 0, s.userGroupErr
	}
	if idx < len(s.userGroupCounts) {
		return s.userGroupCounts[idx], nil
	}
	return 1, nil
}

func (s *userRPMCacheStub) IncrementUserRPM(_ context.Context, _ int64) (int, error) {
	idx := int(atomic.AddInt32(&s.userCalls, 1)) - 1
	if s.userErr != nil {
		return 0, s.userErr
	}
	if idx < len(s.userCounts) {
		return s.userCounts[idx], nil
	}
	return 1, nil
}

func (s *userRPMCacheStub) GetUserGroupRPM(_ context.Context, _, _ int64) (int, error) {
	atomic.AddInt32(&s.userGroupReads, 1)
	return 0, nil
}

func (s *userRPMCacheStub) GetUserRPM(_ context.Context, _ int64) (int, error) {
	atomic.AddInt32(&s.userReads, 1)
	return 0, nil
}

// rpmOverrideRepoStub 专用于 checkRPM 分支测试，只实现必要方法。
type rpmOverrideRepoStub struct {
	UserGroupRateRepository

	override *int
	err      error
	calls    int32
}

func (s *rpmOverrideRepoStub) GetRPMOverrideByUserAndGroup(_ context.Context, _, _ int64) (*int, error) {
	atomic.AddInt32(&s.calls, 1)
	if s.err != nil {
		return nil, s.err
	}
	return s.override, nil
}

func newBillingServiceForRPM(t *testing.T, cache UserRPMCache, rateRepo UserGroupRateRepository) *BillingCacheService {
	t.Helper()
	// 用 nil BillingCache 走 "无缓存" 分支，避免 CheckBillingEligibility 副作用。
	// 我们只直接测 checkRPM。
	svc := NewBillingCacheService(nil, nil, nil, nil, cache, rateRepo, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func TestBillingCacheService_CheckRPM_OverrideTakesPrecedenceOverGroup(t *testing.T) {
	override := 2
	// user-group 计数: 1, 2, 3；user 计数: 默认返回 1（远小于 RPMLimit=100，不干扰）
	cache := &userRPMCacheStub{userGroupCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{override: &override}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 100} // 全局上限设高，不干扰 override 测试
	group := &Group{ID: 10, RPMLimit: 100}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrGroupRPMExceeded)

	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userGroupCalls), "override 命中分支应走 user-group 计数")
	// 用户全局 RPM 在第一个实际候选前先占用；即使随后分组竞争超限，
	// 该客户端请求也已经消耗一次全局 RPM。
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userCalls), "每个客户端请求应先占用一次用户全局 RPM")
	require.EqualValues(t, 3, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_CheckRPM_UserLimitIsGlobalHardCap(t *testing.T) {
	override := 100 // override 很高
	// user-group 计数: 默认返回 1（远小于 override）；user 计数: 1, 2, 3
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{override: &override}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 2} // 全局硬上限=2，应覆盖 override=100
	group := &Group{ID: 10, RPMLimit: 100}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrUserRPMExceeded, "user 全局硬上限应优先于 override")
}

func TestBillingCacheService_CheckRPM_OverrideZeroSkipsGroupButUserStillApplies(t *testing.T) {
	zero := 0
	// user 计数: 依次返回 1..6
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3, 4, 5, 6}}
	repo := &rpmOverrideRepoStub{override: &zero}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 5}
	group := &Group{ID: 10, RPMLimit: 100}

	// override=0 跳过分组计数，但 user.RPMLimit=5 仍生效
	for i := 0; i < 5; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group), "request %d should pass", i+1)
	}
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrUserRPMExceeded,
		"override=0 跳过分组但 user 全局上限仍应生效")
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "override=0 不应触发分组计数器")
	require.EqualValues(t, 6, atomic.LoadInt32(&cache.userCalls), "user 计数器应被调用")
}

func TestBillingCacheService_CheckRPM_OverrideZeroAndUserZeroIsFullyUnlimited(t *testing.T) {
	zero := 0
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{override: &zero}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0} // user 也不限
	group := &Group{ID: 10, RPMLimit: 100}

	for i := 0; i < 50; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group))
	}
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "override=0 不触发分组计数")
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls), "user.RPMLimit=0 也不触发用户计数")
}

func TestBillingCacheService_CheckRPM_NilOverrideFallsThroughToGroup(t *testing.T) {
	// user-group 计数: 5, 6；user 计数: 默认 1（不干扰）
	cache := &userRPMCacheStub{userGroupCounts: []int{5, 6}}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 999} // 全局上限很高，group 先超
	group := &Group{ID: 10, RPMLimit: 5}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))                      // ug=5, user=1, 都没超
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrGroupRPMExceeded) // ug=6 > 5

	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userCalls), "每个客户端请求应先占用一次用户全局 RPM")
}

func TestBillingCacheService_CheckRPM_OverrideLookupErrorFallsThroughToGroup(t *testing.T) {
	cache := &userRPMCacheStub{userGroupCounts: []int{3}}
	repo := &rpmOverrideRepoStub{err: errors.New("db down")}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0}
	group := &Group{ID: 10, RPMLimit: 10}

	// override 查询失败后应继续尝试 group 分支（不直接拒绝）
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 1, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_CheckRPM_UserLevelFallbackWhenGroupUnlimited(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 2}
	group := &Group{ID: 10, RPMLimit: 0} // 分组未设限

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrUserRPMExceeded)

	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls), "group 未设限时不应 INCR user-group 键")
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_NoLimitsConfiguredIsNoop(t *testing.T) {
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0}
	group := &Group{ID: 10, RPMLimit: 0}

	for i := 0; i < 10; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group))
	}
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_RedisErrorFailOpen(t *testing.T) {
	cache := &userRPMCacheStub{userGroupErr: errors.New("redis unavailable")}
	repo := &rpmOverrideRepoStub{override: nil}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 0}
	group := &Group{ID: 10, RPMLimit: 5}

	// Redis 故障时应 fail-open，不拒绝请求
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestBillingCacheService_CheckRPM_NoGroupUsesUserOnly(t *testing.T) {
	cache := &userRPMCacheStub{userCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 2}

	// 无 group（纯用户级限流场景），不应查询 rpm_override。
	require.NoError(t, svc.checkRPM(context.Background(), user, nil))
	require.NoError(t, svc.checkRPM(context.Background(), user, nil))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, nil), ErrUserRPMExceeded)

	require.EqualValues(t, 0, atomic.LoadInt32(&repo.calls), "无 group 时不应查询 rpm_override")
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_NilUserIsNoop(t *testing.T) {
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	require.NoError(t, svc.checkRPM(context.Background(), nil, &Group{ID: 1, RPMLimit: 10}))
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userGroupCalls))
	require.EqualValues(t, 0, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 0, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_MultiGroupReadOnlyScanDoesNotIncrementRPM(t *testing.T) {
	cache := &userRPMCacheStub{}
	svc := newBillingServiceForRPM(t, cache, nil)
	user := &User{ID: 1, RPMLimit: 100, UserGroupRPMOverrideResolved: true}

	for groupID := int64(1); groupID <= 10; groupID++ {
		require.NoError(t, svc.checkRPMReadOnly(
			context.Background(),
			user,
			&Group{ID: groupID, RPMLimit: 100},
		))
	}

	require.EqualValues(t, 10, atomic.LoadInt32(&cache.userGroupReads))
	require.EqualValues(t, 10, atomic.LoadInt32(&cache.userReads))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_MultiGroupFollowupProbeSkipsReservedUserRPM(t *testing.T) {
	cache := &userRPMCacheStub{}
	svc := newBillingServiceForRPM(t, cache, nil)
	// RouteRuntime passes an independent user snapshot with RPMLimit cleared
	// after the first actual candidate has reserved the request-global slot.
	user := &User{ID: 1, RPMLimit: 0, UserGroupRPMOverrideResolved: true}
	group := &Group{ID: 12, RPMLimit: 100}

	require.NoError(t, svc.checkRPMReadOnly(context.Background(), user, group))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupReads))
	require.Zero(t, atomic.LoadInt32(&cache.userReads))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_MultiGroupActualAttemptsReserveUserOnceAndGroupsOnce(t *testing.T) {
	cache := &userRPMCacheStub{}
	svc := newBillingServiceForRPM(t, cache, nil)
	user := &User{ID: 1, RPMLimit: 100, UserGroupRPMOverrideResolved: true}
	groupA := &Group{ID: 11, RPMLimit: 100}
	groupB := &Group{ID: 12, RPMLimit: 100}

	require.NoError(t, svc.ReserveUserRPM(context.Background(), user))
	require.NoError(t, svc.ReserveGroupRPM(context.Background(), user, groupA))
	require.NoError(t, svc.ReserveGroupRPM(context.Background(), user, groupB))

	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userCalls))
	require.EqualValues(t, 2, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestBillingCacheService_SimpleModeMultiGroupSkipsRPMReadsAndReservations(t *testing.T) {
	cache := &userRPMCacheStub{}
	override := 1
	repo := &rpmOverrideRepoStub{override: &override}
	svc := NewBillingCacheService(
		nil, nil, nil, nil, cache, repo,
		&config.Config{RunMode: config.RunModeSimple}, nil,
	)
	t.Cleanup(svc.Stop)
	user := &User{ID: 1, RPMLimit: 1}
	// Keep the minimum-balance gate disabled so this test isolates Simple Mode's
	// RPM behavior. Minimum balance intentionally remains active in Simple Mode
	// and is covered by billing_cache_service_balance_test.go.
	group := &Group{ID: 11, RPMLimit: 1}

	require.NoError(t, svc.CheckBillingEligibilityReadOnly(
		context.Background(), user, &APIKey{ID: 2}, group, nil, PlatformOpenAI,
	))
	require.NoError(t, svc.ReserveUserRPM(context.Background(), user))
	require.NoError(t, svc.ReserveGroupRPM(context.Background(), user, group))

	require.Zero(t, atomic.LoadInt32(&cache.userReads))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupReads))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_LegacySingleGroupReservesGroupBeforeUser(t *testing.T) {
	cache := &userRPMCacheStub{userGroupCounts: []int{2}}
	svc := newBillingServiceForRPM(t, cache, nil)
	user := &User{
		ID:                           1,
		RPMLimit:                     100,
		UserGroupRPMOverrideResolved: true,
	}
	group := &Group{ID: 10, RPMLimit: 1}

	require.ErrorIs(t, svc.reserveLegacyRPM(context.Background(), user, group), ErrGroupRPMExceeded)
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls),
		"legacy single-group rejection must not consume the user-global RPM counter")
}
