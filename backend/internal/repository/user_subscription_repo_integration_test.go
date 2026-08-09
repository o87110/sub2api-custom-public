//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionbulkreset"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionrepository"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/suite"
)

type UserSubscriptionRepoSuite struct {
	suite.Suite
	ctx    context.Context
	client *dbent.Client
	repo   *subscriptionrepository.Repository
}

func (s *UserSubscriptionRepoSuite) SetupTest() {
	ctx := context.Background()
	tx := testEntTx(s.T())
	s.ctx = dbent.NewTxContext(ctx, tx)
	s.client = tx.Client()
	s.repo = subscriptionrepository.New(s.client, NewUserSubscriptionRepository(s.client))
}

func TestUserSubscriptionRepoSuite(t *testing.T) {
	suite.Run(t, new(UserSubscriptionRepoSuite))
}

func (s *UserSubscriptionRepoSuite) mustCreateUser(email string, role string) *service.User {
	s.T().Helper()

	if role == "" {
		role = service.RoleUser
	}

	u, err := s.client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetStatus(service.StatusActive).
		SetRole(role).
		Save(s.ctx)
	s.Require().NoError(err, "create user")
	return userEntityToService(u)
}

func (s *UserSubscriptionRepoSuite) mustCreateGroup(name string) *service.Group {
	s.T().Helper()

	g, err := s.client.Group.Create().
		SetName(name).
		SetStatus(service.StatusActive).
		Save(s.ctx)
	s.Require().NoError(err, "create group")
	return groupEntityToService(g)
}

func (s *UserSubscriptionRepoSuite) mustCreateSubscription(userID, groupID int64, mutate func(*dbent.UserSubscriptionCreate)) *dbent.UserSubscription {
	return s.mustCreateSubscriptionWithCycle(userID, groupID, mutate, nil)
}

type subscriptionCycleFixture struct {
	startsAt                    time.Time
	endsAt                      time.Time
	sourceType                  string
	sourceRef                   *string
	manualBulkQuotaResetEnabled bool
}

func (s *UserSubscriptionRepoSuite) mustCreateSubscriptionWithCycle(
	userID, groupID int64,
	mutate func(*dbent.UserSubscriptionCreate),
	cycle *subscriptionCycleFixture,
) *dbent.UserSubscription {
	s.T().Helper()

	now := time.Now()
	create := s.client.UserSubscription.Create().
		SetUserID(userID).
		SetGroupID(groupID).
		SetStartsAt(now.Add(-1 * time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetAssignedAt(now).
		SetNotes("")

	if mutate != nil {
		mutate(create)
	}

	sub, err := create.Save(s.ctx)
	s.Require().NoError(err, "create user subscription")
	cycleStartsAt := sub.StartsAt
	cycleEndsAt := sub.ExpiresAt
	cycleSourceType := subscriptionquota.CycleSourceAssignment
	var cycleSourceRef *string
	manualBulkQuotaResetEnabled := false
	if cycle != nil {
		cycleStartsAt = cycle.startsAt
		cycleEndsAt = cycle.endsAt
		cycleSourceType = cycle.sourceType
		cycleSourceRef = cycle.sourceRef
		manualBulkQuotaResetEnabled = cycle.manualBulkQuotaResetEnabled
	}
	err = subscriptionquota.InitializeCurrentCycle(
		s.ctx,
		s.client,
		sub.ID,
		cycleStartsAt,
		cycleEndsAt,
		sub.CycleUsageUsd,
		sub.ManualQuotaResetCount,
		cycleSourceType,
		cycleSourceRef,
		manualBulkQuotaResetEnabled,
	)
	s.Require().NoError(err, "create current subscription cycle")
	sub.CurrentCycleStartsAt = cycleStartsAt
	sub.CurrentCycleEndsAt = cycleEndsAt
	return sub
}

// --- Create / GetByID / Update / Delete ---

func (s *UserSubscriptionRepoSuite) TestCreate() {
	user := s.mustCreateUser("sub-create@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-create")

	sub := &service.UserSubscription{
		UserID:    user.ID,
		GroupID:   group.ID,
		Status:    service.SubscriptionStatusActive,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	err := s.repo.Create(s.ctx, sub)
	s.Require().NoError(err, "Create")
	s.Require().NotZero(sub.ID, "expected ID to be set")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().Equal(sub.UserID, got.UserID)
	s.Require().Equal(sub.GroupID, got.GroupID)
}

func (s *UserSubscriptionRepoSuite) TestGetByID_WithPreloads() {
	user := s.mustCreateUser("preload@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-preload")
	admin := s.mustCreateUser("admin@test.com", service.RoleAdmin)

	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetAssignedBy(admin.ID)
	})

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().NotNil(got.User, "expected User preload")
	s.Require().NotNil(got.Group, "expected Group preload")
	s.Require().NotNil(got.AssignedByUser, "expected AssignedByUser preload")
	s.Require().Equal(user.ID, got.User.ID)
	s.Require().Equal(group.ID, got.Group.ID)
	s.Require().Equal(admin.ID, got.AssignedByUser.ID)
}

func (s *UserSubscriptionRepoSuite) TestGetByID_NotFound() {
	_, err := s.repo.GetByID(s.ctx, 999999)
	s.Require().Error(err, "expected error for non-existent ID")
}

func (s *UserSubscriptionRepoSuite) TestUpdate() {
	user := s.mustCreateUser("update@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-update")
	created := s.mustCreateSubscription(user.ID, group.ID, nil)

	sub, err := s.repo.GetByID(s.ctx, created.ID)
	s.Require().NoError(err, "GetByID")

	sub.Notes = "updated notes"
	s.Require().NoError(s.repo.Update(s.ctx, sub), "Update")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID after update")
	s.Require().Equal("updated notes", got.Notes)
}

func (s *UserSubscriptionRepoSuite) TestDelete() {
	user := s.mustCreateUser("delete@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-delete")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	err := s.repo.Delete(s.ctx, sub.ID)
	s.Require().NoError(err, "Delete")

	_, err = s.repo.GetByID(s.ctx, sub.ID)
	s.Require().Error(err, "expected error after delete")
}

func (s *UserSubscriptionRepoSuite) TestGetByIDIncludeDeleted_PreservesPersistedStatus() {
	user := s.mustCreateUser("include-deleted@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-include-deleted")
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusActive)
	})

	s.Require().NoError(s.repo.Delete(s.ctx, sub.ID), "Delete")

	got, err := s.repo.GetByIDIncludeDeleted(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByIDIncludeDeleted")
	s.Require().Equal(service.SubscriptionStatusActive, got.Status)
	s.Require().NotNil(got.DeletedAt)
	s.Require().NotNil(got.User)
	s.Require().NotNil(got.Group)
}

func (s *UserSubscriptionRepoSuite) TestRestore() {
	user := s.mustCreateUser("restore@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-restore")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	s.Require().NoError(s.repo.Delete(s.ctx, sub.ID), "Delete")

	restored, err := s.repo.Restore(s.ctx, sub.ID, service.SubscriptionStatusExpired)
	s.Require().NoError(err, "Restore")
	s.Require().Equal(service.SubscriptionStatusExpired, restored.Status)
	s.Require().Nil(restored.DeletedAt)

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err, "GetByID after restore")
	s.Require().Nil(got.DeletedAt)
	s.Require().Equal(service.SubscriptionStatusExpired, got.Status)
}

func (s *UserSubscriptionRepoSuite) TestDelete_Idempotent() {
	s.Require().NoError(s.repo.Delete(s.ctx, 42424242), "Delete should be idempotent")
}

// --- GetByUserIDAndGroupID / GetActiveByUserIDAndGroupID ---

func (s *UserSubscriptionRepoSuite) TestGetByUserIDAndGroupID() {
	user := s.mustCreateUser("byuser@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-byuser")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	got, err := s.repo.GetByUserIDAndGroupID(s.ctx, user.ID, group.ID)
	s.Require().NoError(err, "GetByUserIDAndGroupID")
	s.Require().Equal(sub.ID, got.ID)
	s.Require().NotNil(got.Group, "expected Group preload")
}

func (s *UserSubscriptionRepoSuite) TestGetByUserIDAndGroupID_NotFound() {
	_, err := s.repo.GetByUserIDAndGroupID(s.ctx, 999999, 999999)
	s.Require().Error(err, "expected error for non-existent pair")
}

func (s *UserSubscriptionRepoSuite) TestGetActiveByUserIDAndGroupID() {
	user := s.mustCreateUser("active@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-active")

	active := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(2 * time.Hour))
	})

	got, err := s.repo.GetActiveByUserIDAndGroupID(s.ctx, user.ID, group.ID)
	s.Require().NoError(err, "GetActiveByUserIDAndGroupID")
	s.Require().Equal(active.ID, got.ID)
}

func (s *UserSubscriptionRepoSuite) TestGetActiveByUserIDAndGroupID_ExpiredIgnored() {
	user := s.mustCreateUser("expired@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-expired")

	s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-2 * time.Hour))
	})

	_, err := s.repo.GetActiveByUserIDAndGroupID(s.ctx, user.ID, group.ID)
	s.Require().Error(err, "expected error for expired subscription")
}

// --- ListByUserID / ListActiveByUserID ---

func (s *UserSubscriptionRepoSuite) TestListByUserID() {
	user := s.mustCreateUser("listby@test.com", service.RoleUser)
	g1 := s.mustCreateGroup("g-list1")
	g2 := s.mustCreateGroup("g-list2")

	s.mustCreateSubscription(user.ID, g1.ID, nil)
	s.mustCreateSubscription(user.ID, g2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	subs, err := s.repo.ListByUserID(s.ctx, user.ID)
	s.Require().NoError(err, "ListByUserID")
	s.Require().Len(subs, 2)
	for _, sub := range subs {
		s.Require().NotNil(sub.Group, "expected Group preload")
	}
}

func (s *UserSubscriptionRepoSuite) TestListActiveByUserID() {
	user := s.mustCreateUser("listactive@test.com", service.RoleUser)
	g1 := s.mustCreateGroup("g-act1")
	g2 := s.mustCreateGroup("g-act2")

	s.mustCreateSubscription(user.ID, g1.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user.ID, g2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	subs, err := s.repo.ListActiveByUserID(s.ctx, user.ID)
	s.Require().NoError(err, "ListActiveByUserID")
	s.Require().Len(subs, 1)
	s.Require().Equal(service.SubscriptionStatusActive, subs[0].Status)
}

// --- ListByGroupID ---

func (s *UserSubscriptionRepoSuite) TestListByGroupID() {
	user1 := s.mustCreateUser("u1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("u2@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-listgrp")

	s.mustCreateSubscription(user1.ID, group.ID, nil)
	s.mustCreateSubscription(user2.ID, group.ID, nil)

	subs, page, err := s.repo.ListByGroupID(s.ctx, group.ID, pagination.PaginationParams{Page: 1, PageSize: 10})
	s.Require().NoError(err, "ListByGroupID")
	s.Require().Len(subs, 2)
	s.Require().Equal(int64(2), page.Total)
	for _, sub := range subs {
		s.Require().NotNil(sub.User, "expected User preload")
		s.Require().NotNil(sub.Group, "expected Group preload")
	}
}

// --- List with filters ---

func (s *UserSubscriptionRepoSuite) TestList_NoFilters() {
	user := s.mustCreateUser("list@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-list")
	s.mustCreateSubscription(user.ID, group.ID, nil)

	subs, page, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, nil, "", "", "", "")
	s.Require().NoError(err, "List")
	s.Require().Len(subs, 1)
	s.Require().Equal(int64(1), page.Total)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByUserID() {
	user1 := s.mustCreateUser("filter1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("filter2@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-filter")

	s.mustCreateSubscription(user1.ID, group.ID, nil)
	s.mustCreateSubscription(user2.ID, group.ID, nil)

	subs, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, &user1.ID, nil, "", "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(user1.ID, subs[0].UserID)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByGroupID() {
	user := s.mustCreateUser("grpfilter@test.com", service.RoleUser)
	g1 := s.mustCreateGroup("g-f1")
	g2 := s.mustCreateGroup("g-f2")

	s.mustCreateSubscription(user.ID, g1.ID, nil)
	s.mustCreateSubscription(user.ID, g2.ID, nil)

	subs, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, &g1.ID, "", "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(g1.ID, subs[0].GroupID)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByStatus() {
	user1 := s.mustCreateUser("statfilter1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("statfilter2@test.com", service.RoleUser)
	group1 := s.mustCreateGroup("g-stat-1")
	group2 := s.mustCreateGroup("g-stat-2")

	s.mustCreateSubscription(user1.ID, group1.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusActive)
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user2.ID, group2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	subs, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, nil, service.SubscriptionStatusExpired, "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(service.SubscriptionStatusExpired, subs[0].Status)
}

func (s *UserSubscriptionRepoSuite) TestList_IncludesRevokedWhenStatusEmpty() {
	user1 := s.mustCreateUser("allstatus1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("allstatus2@test.com", service.RoleUser)
	user3 := s.mustCreateUser("allstatus3@test.com", service.RoleUser)
	group1 := s.mustCreateGroup("g-allstatus-1")
	group2 := s.mustCreateGroup("g-allstatus-2")
	group3 := s.mustCreateGroup("g-allstatus-3")

	s.mustCreateSubscription(user1.ID, group1.ID, nil)
	s.mustCreateSubscription(user2.ID, group2.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})
	revoked := s.mustCreateSubscription(user3.ID, group3.ID, nil)
	s.Require().NoError(s.repo.Delete(s.ctx, revoked.ID))

	subs, pag, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, nil, "", "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 3)
	s.Require().Equal(int64(3), pag.Total)

	var gotRevoked *service.UserSubscription
	for i := range subs {
		if subs[i].ID == revoked.ID {
			gotRevoked = &subs[i]
			break
		}
	}
	s.Require().NotNil(gotRevoked, "all status should include soft-deleted subscription")
	s.Require().Equal(service.SubscriptionStatusRevoked, gotRevoked.Status)
	s.Require().NotNil(gotRevoked.DeletedAt)
	s.Require().NotNil(gotRevoked.User)
	s.Require().NotNil(gotRevoked.Group)
}

func (s *UserSubscriptionRepoSuite) TestList_FilterByRevokedStatus() {
	user1 := s.mustCreateUser("revokedfilter1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("revokedfilter2@test.com", service.RoleUser)
	group1 := s.mustCreateGroup("g-revoked-1")
	group2 := s.mustCreateGroup("g-revoked-2")

	active := s.mustCreateSubscription(user1.ID, group1.ID, nil)
	revoked := s.mustCreateSubscription(user2.ID, group2.ID, nil)
	s.Require().NoError(s.repo.Delete(s.ctx, revoked.ID))

	subs, pag, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, nil, nil, service.SubscriptionStatusRevoked, "", "", "")
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	s.Require().Equal(int64(1), pag.Total)
	s.Require().Equal(revoked.ID, subs[0].ID)
	s.Require().NotEqual(active.ID, subs[0].ID)
	s.Require().Equal(service.SubscriptionStatusRevoked, subs[0].Status)
	s.Require().NotNil(subs[0].DeletedAt)
}

// --- Usage tracking ---

func (s *UserSubscriptionRepoSuite) TestIncrementUsage() {
	user := s.mustCreateUser("usage@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-usage")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	err := s.repo.IncrementUsage(s.ctx, sub.ID, 1.25)
	s.Require().NoError(err, "IncrementUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(1.25, got.DailyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, got.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, got.MonthlyUsageUSD, 1e-6)
}

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_Accumulates() {
	user := s.mustCreateUser("accum@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-accum")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	s.Require().NoError(s.repo.IncrementUsage(s.ctx, sub.ID, 1.0))
	s.Require().NoError(s.repo.IncrementUsage(s.ctx, sub.ID, 2.5))

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(3.5, got.DailyUsageUSD, 1e-6)
	s.Require().InDelta(3.5, got.CycleUsageUSD, 1e-6)
}

func (s *UserSubscriptionRepoSuite) TestActivateWindows() {
	user := s.mustCreateUser("activate@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-activate")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	dailyStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	activateAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	err := s.repo.ActivateWindows(s.ctx, sub.ID, dailyStart, activateAt)
	s.Require().NoError(err, "ActivateWindows")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().NotNil(got.DailyWindowStart)
	s.Require().NotNil(got.WeeklyWindowStart)
	s.Require().NotNil(got.MonthlyWindowStart)
	s.Require().WithinDuration(dailyStart, *got.DailyWindowStart, time.Microsecond)
	s.Require().WithinDuration(activateAt, *got.WeeklyWindowStart, time.Microsecond)
	s.Require().WithinDuration(activateAt, *got.MonthlyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestActivateWindows_StaleActivationPreservesExistingWindows() {
	user := s.mustCreateUser("activate-cas@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-activate-cas")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)
	activatedAt := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	manualResetAt := activatedAt.Add(2 * time.Hour)
	manualDailyStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	s.Require().NoError(s.repo.ActivateWindows(s.ctx, sub.ID, activatedAt, activatedAt))
	s.Require().NoError(s.repo.ResetUsageWindows(s.ctx, sub.ID, true, true, true, manualDailyStart, manualResetAt))
	// Simulate a concurrent request carrying the original unactivated snapshot.
	s.Require().NoError(s.repo.ActivateWindows(s.ctx, sub.ID, activatedAt.Add(time.Hour), activatedAt.Add(time.Hour)))

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(manualDailyStart, *got.DailyWindowStart, time.Microsecond)
	s.Require().WithinDuration(manualResetAt, *got.WeeklyWindowStart, time.Microsecond)
	s.Require().WithinDuration(manualResetAt, *got.MonthlyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetDailyUsage() {
	user := s.mustCreateUser("resetd@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-resetd")
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyUsageUsd(10.0)
		c.SetWeeklyUsageUsd(20.0)
	})

	resetAt := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	err := s.repo.ResetDailyUsage(s.ctx, sub.ID, sub.DailyWindowStart, resetAt)
	s.Require().NoError(err, "ResetDailyUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.0, got.DailyUsageUSD, 1e-6)
	s.Require().InDelta(20.0, got.WeeklyUsageUSD, 1e-6)
	s.Require().NotNil(got.DailyWindowStart)
	s.Require().WithinDuration(resetAt, *got.DailyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetDailyUsage_StaleResetDoesNotClearNewWindowUsage() {
	user := s.mustCreateUser("resetd-cas@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-resetd-cas")
	oldWindowStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyWindowStart(oldWindowStart)
		c.SetDailyUsageUsd(10)
	})

	newWindowStart := oldWindowStart.Add(24 * time.Hour)
	s.Require().NoError(s.repo.ResetDailyUsage(s.ctx, sub.ID, &oldWindowStart, newWindowStart))
	s.Require().NoError(s.repo.IncrementUsage(s.ctx, sub.ID, 3))
	// Simulate a second request carrying the stale old-window snapshot.
	s.Require().NoError(s.repo.ResetDailyUsage(s.ctx, sub.ID, &oldWindowStart, newWindowStart))

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(3, got.DailyUsageUSD, 1e-6)
	s.Require().WithinDuration(newWindowStart, *got.DailyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetUsageWindows_ClearsUsageAfterAutomaticWindowAdvance() {
	user := s.mustCreateUser("admin-reset-current@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-admin-reset-current")
	oldWindowStart := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyWindowStart(oldWindowStart)
		c.SetDailyUsageUsd(10)
	})

	newWindowStart := oldWindowStart.Add(24 * time.Hour)
	s.Require().NoError(s.repo.ResetDailyUsage(s.ctx, sub.ID, &oldWindowStart, newWindowStart))
	s.Require().NoError(s.repo.IncrementUsage(s.ctx, sub.ID, 3))
	s.Require().NoError(s.repo.ResetUsageWindows(s.ctx, sub.ID, true, false, false, newWindowStart, newWindowStart))

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0, got.DailyUsageUSD, 1e-6)
	s.Require().InDelta(3, got.CycleUsageUSD, 1e-6)
	s.Require().Equal(int64(1), got.ManualQuotaResetCount)
	s.Require().WithinDuration(newWindowStart, *got.DailyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestAdvanceCycleAfterEarlyRenewal() {
	user := s.mustCreateUser("advance-cycle@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-advance-cycle")
	currentEndsAt := time.Now().Add(24 * time.Hour)
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(currentEndsAt)
		c.SetDailyUsageUsd(8)
		c.SetWeeklyUsageUsd(8)
		c.SetMonthlyUsageUsd(8)
		c.SetCycleUsageUsd(12.5)
		c.SetManualQuotaResetCount(2)
	})
	nextEndsAt := currentEndsAt.Add(30 * 24 * time.Hour)
	s.Require().NoError(s.repo.AppendRenewalCycle(
		s.ctx,
		sub.ID,
		currentEndsAt,
		nextEndsAt,
		subscriptionquota.CycleSourceRedeem,
		nil,
	))

	advanceAt := currentEndsAt.Add(time.Minute)
	advanced, err := s.repo.AdvanceCycle(s.ctx, sub.ID, advanceAt, timezone.StartOfDay(advanceAt))
	s.Require().NoError(err)
	s.Require().True(advanced)

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(currentEndsAt, got.CurrentCycleStartsAt, time.Microsecond)
	s.Require().WithinDuration(nextEndsAt, got.CurrentCycleEndsAt, time.Microsecond)
	s.Require().Zero(got.CycleUsageUSD)
	s.Require().Zero(got.ManualQuotaResetCount)
	s.Require().Zero(got.DailyUsageUSD)
}

func (s *UserSubscriptionRepoSuite) TestExtendExpiryRestoresLatestCompletedCycleForExpiredSubscription() {
	user := s.mustCreateUser("extend-expired-cycle@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-extend-expired-cycle")
	now := time.Now().UTC()
	startsAt := now.Add(-30 * 24 * time.Hour)
	oldEndsAt := now.Add(-time.Hour)
	newEndsAt := now.Add(30 * 24 * time.Hour)
	sub := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStartsAt(startsAt).
			SetExpiresAt(oldEndsAt).
			SetStatus(service.SubscriptionStatusExpired).
			SetCurrentCycleStartsAt(startsAt).
			SetCurrentCycleEndsAt(oldEndsAt).
			SetCycleUsageUsd(17.25).
			SetManualQuotaResetCount(2)
	}, &subscriptionCycleFixture{
		startsAt:   startsAt,
		endsAt:     oldEndsAt,
		sourceType: subscriptionquota.CycleSourceAssignment,
	})
	completedAt := oldEndsAt
	snapshot, err := s.repo.CaptureTermSnapshot(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Len(snapshot.Cycles, 1)
	snapshot.Expected, err = subscriptionquota.CaptureExpectedTermState(s.ctx, s.client, sub.ID)
	s.Require().NoError(err)
	snapshot.Cycles[0].Status = subscriptionquota.CycleStatusCompleted
	snapshot.Cycles[0].FinalUsageUSD = 17.25
	snapshot.Cycles[0].FinalManualQuotaResetCount = 2
	snapshot.Cycles[0].CompletedAt = &completedAt
	s.Require().NoError(s.repo.RestoreTermSnapshot(s.ctx, snapshot))

	s.Require().NoError(s.repo.ExtendExpiry(s.ctx, sub.ID, newEndsAt))

	restoredSnapshot, err := s.repo.CaptureTermSnapshot(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Len(restoredSnapshot.Cycles, 1)
	restored := restoredSnapshot.Cycles[0]
	s.Require().Equal(subscriptionquota.CycleStatusCurrent, restored.Status)
	s.Require().WithinDuration(newEndsAt, restored.EndsAt, time.Microsecond)
	s.Require().Nil(restored.CompletedAt)
	s.Require().Zero(restored.FinalUsageUSD)
	s.Require().Zero(restored.FinalManualQuotaResetCount)
	updated, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(startsAt, updated.CurrentCycleStartsAt, time.Microsecond)
	s.Require().WithinDuration(newEndsAt, updated.CurrentCycleEndsAt, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestRestoreTermSnapshotRestoresCancelledRenewalAttribution() {
	user := s.mustCreateUser("refund-cycle-restore@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-refund-cycle-restore")
	now := time.Now().UTC()
	currentStartsAt := now.Add(-25 * 24 * time.Hour)
	currentEndsAt := now.Add(5 * 24 * time.Hour)
	pendingEndsAt := currentEndsAt.Add(30 * 24 * time.Hour)
	sub := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStartsAt(currentStartsAt).
			SetExpiresAt(pendingEndsAt).
			SetCurrentCycleStartsAt(currentStartsAt).
			SetCurrentCycleEndsAt(currentEndsAt)
	}, &subscriptionCycleFixture{
		startsAt:   currentStartsAt,
		endsAt:     currentEndsAt,
		sourceType: subscriptionquota.CycleSourcePayment,
		sourceRef:  stringPointer("1001"),
	})
	s.Require().NoError(s.repo.AppendRenewalCycle(
		s.ctx,
		sub.ID,
		currentEndsAt,
		pendingEndsAt,
		subscriptionquota.CycleSourcePayment,
		stringPointer("1002"),
	))

	shortenedEndsAt := currentEndsAt.Add(-24 * time.Hour)
	snapshot, err := s.repo.AdjustExpiryWithSnapshot(s.ctx, sub.ID, shortenedEndsAt)
	s.Require().NoError(err)
	adjustedSnapshot, err := s.repo.CaptureTermSnapshot(s.ctx, sub.ID)
	s.Require().NoError(err)
	cancelledIndex, cancelled := findCycleSnapshotBySourceRef(adjustedSnapshot, "1002")
	s.Require().NotEqual(-1, cancelledIndex)
	s.Require().Equal(subscriptionquota.CycleStatusCancelled, cancelled.Status)

	s.Require().NoError(s.repo.RestoreTermSnapshot(s.ctx, snapshot))

	restoredSub, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(pendingEndsAt, restoredSub.ExpiresAt, time.Microsecond)
	s.Require().WithinDuration(currentStartsAt, restoredSub.CurrentCycleStartsAt, time.Microsecond)
	s.Require().WithinDuration(currentEndsAt, restoredSub.CurrentCycleEndsAt, time.Microsecond)
	restoredSnapshot, err := s.repo.CaptureTermSnapshot(s.ctx, sub.ID)
	s.Require().NoError(err)
	currentIndex, restoredCurrent := findCycleSnapshotBySourceRef(restoredSnapshot, "1001")
	pendingIndex, restoredPending := findCycleSnapshotBySourceRef(restoredSnapshot, "1002")
	s.Require().NotEqual(-1, currentIndex)
	s.Require().NotEqual(-1, pendingIndex)
	s.Require().Equal(subscriptionquota.CycleStatusCurrent, restoredCurrent.Status)
	s.Require().Equal(subscriptionquota.CycleSourcePayment, restoredCurrent.SourceType)
	s.Require().Equal("1001", *restoredCurrent.SourceRef)
	s.Require().WithinDuration(currentEndsAt, restoredCurrent.EndsAt, time.Microsecond)
	s.Require().Equal(subscriptionquota.CycleStatusPending, restoredPending.Status)
	s.Require().Equal(subscriptionquota.CycleSourcePayment, restoredPending.SourceType)
	s.Require().Equal("1002", *restoredPending.SourceRef)
	s.Require().WithinDuration(pendingEndsAt, restoredPending.EndsAt, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetUsageWindowsIdempotentOnlyIncrementsResetCountOnce() {
	user := s.mustCreateUser("idempotent-reset@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-idempotent-reset")
	now := time.Now().UTC()
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyUsageUsd(8).
			SetWeeklyUsageUsd(9).
			SetMonthlyUsageUsd(10)
	})
	operation, err := idempotencyexecution.New("admin.subscriptions.bulk_reset_quota", "admin:1", service.HashIdempotencyKey("same-bulk-operation"), now, now.Add(24*time.Hour))
	s.Require().NoError(err)

	applied, err := s.repo.ResetUsageWindowsIdempotent(s.ctx, sub.ID, true, true, true, now, now, operation)
	s.Require().NoError(err)
	s.Require().True(applied)
	applied, err = s.repo.ResetUsageWindowsIdempotent(s.ctx, sub.ID, true, true, true, now, now, operation)
	s.Require().NoError(err)
	s.Require().False(applied)

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Zero(got.DailyUsageUSD)
	s.Require().Zero(got.WeeklyUsageUSD)
	s.Require().Zero(got.MonthlyUsageUSD)
	s.Require().Equal(int64(1), got.ManualQuotaResetCount)
}

func (s *UserSubscriptionRepoSuite) TestResetQuotaIdempotencyClaimCommitsWithSideEffects() {
	user := s.mustCreateUser("application-idempotent-reset@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-application-idempotent-reset")
	now := time.Now().UTC()
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyUsageUsd(3).
			SetWeeklyUsageUsd(4).
			SetMonthlyUsageUsd(5)
	})
	operation, err := idempotencyexecution.New("admin.subscriptions.reset_quota", "admin:1", service.HashIdempotencyKey("same-single-reset-operation"), now, now.Add(24*time.Hour))
	s.Require().NoError(err)

	first, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &operation, now)
	s.Require().NoError(err)
	second, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &operation, now.Add(time.Second))
	s.Require().NoError(err)

	s.Require().Equal(int64(1), first.ManualQuotaResetCount)
	s.Require().Equal(int64(1), second.ManualQuotaResetCount)
	s.Require().Zero(second.DailyUsageUSD)
	s.Require().Zero(second.WeeklyUsageUSD)
	s.Require().Zero(second.MonthlyUsageUSD)
}

func (s *UserSubscriptionRepoSuite) TestResetQuotaIdempotencySeparatesScopesAndReclaimsExpiredClaim() {
	user := s.mustCreateUser("scoped-idempotent-reset@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-scoped-idempotent-reset")
	now := time.Now().UTC()
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetDailyUsageUsd(3).
			SetWeeklyUsageUsd(4).
			SetMonthlyUsageUsd(5)
	})
	keyHash := service.HashIdempotencyKey("shared-reset-key")
	single, err := idempotencyexecution.New("admin.subscriptions.reset_quota", "admin:9", keyHash, now, now.Add(time.Hour))
	s.Require().NoError(err)
	bulk, err := idempotencyexecution.New("admin.subscriptions.bulk_reset_quota", "admin:9", keyHash, now.Add(time.Minute), now.Add(2*time.Hour))
	s.Require().NoError(err)
	s.Require().NotEqual(single.OperationKeyHash, bulk.OperationKeyHash)

	first, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &single, now)
	s.Require().NoError(err)
	s.Require().Equal(int64(1), first.ManualQuotaResetCount)
	first.DailyUsageUSD = 6
	first.WeeklyUsageUSD = 7
	first.MonthlyUsageUSD = 8
	err = s.repo.Update(s.ctx, first)
	s.Require().NoError(err)

	second, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &bulk, now.Add(time.Minute))
	s.Require().NoError(err)
	s.Require().Equal(int64(2), second.ManualQuotaResetCount)

	retry := bulk
	retry.ClaimedAt = now.Add(2 * time.Minute)
	retry.ExpiresAt = now.Add(3 * time.Hour)
	replayed, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &retry, retry.ClaimedAt)
	s.Require().NoError(err)
	s.Require().Equal(int64(2), replayed.ManualQuotaResetCount)
	replayed.DailyUsageUSD = 9
	replayed.WeeklyUsageUSD = 10
	replayed.MonthlyUsageUSD = 11
	err = s.repo.Update(s.ctx, replayed)
	s.Require().NoError(err)

	beforeExtendedExpiry := retry
	beforeExtendedExpiry.ClaimedAt = bulk.ExpiresAt.Add(time.Minute)
	beforeExtendedExpiry.ExpiresAt = retry.ExpiresAt
	stillReplayed, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &beforeExtendedExpiry, beforeExtendedExpiry.ClaimedAt)
	s.Require().NoError(err)
	s.Require().Equal(int64(2), stillReplayed.ManualQuotaResetCount)
	s.Require().InDelta(9, stillReplayed.DailyUsageUSD, 1e-6)
	s.Require().InDelta(10, stillReplayed.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(11, stillReplayed.MonthlyUsageUSD, 1e-6)

	reclaimed := retry
	reclaimed.ClaimedAt = retry.ExpiresAt.Add(time.Minute)
	reclaimed.ExpiresAt = reclaimed.ClaimedAt.Add(24 * time.Hour)
	third, err := s.repo.ResetQuota(s.ctx, sub.ID, true, true, true, &reclaimed, reclaimed.ClaimedAt)
	s.Require().NoError(err)
	s.Require().Equal(int64(3), third.ManualQuotaResetCount)
	s.Require().Zero(third.DailyUsageUSD)
	s.Require().Zero(third.WeeklyUsageUSD)
	s.Require().Zero(third.MonthlyUsageUSD)
}

func (s *UserSubscriptionRepoSuite) TestRestoreTermSnapshotRejectsConcurrentCycleAdvance() {
	user := s.mustCreateUser("cycle-cas-advance@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-cycle-cas-advance")
	now := time.Now().UTC()
	boundary := now.Add(time.Hour)
	startsAt := boundary.Add(-30 * 24 * time.Hour)
	nextEndsAt := boundary.Add(30 * 24 * time.Hour)
	sub := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStartsAt(startsAt).
			SetExpiresAt(nextEndsAt).
			SetCurrentCycleStartsAt(startsAt).
			SetCurrentCycleEndsAt(boundary)
	}, &subscriptionCycleFixture{
		startsAt:   startsAt,
		endsAt:     boundary,
		sourceType: subscriptionquota.CycleSourcePayment,
		sourceRef:  stringPointer("cycle-cas-current"),
	})
	s.Require().NoError(s.repo.AppendRenewalCycle(
		s.ctx,
		sub.ID,
		boundary,
		nextEndsAt,
		subscriptionquota.CycleSourcePayment,
		stringPointer("cycle-cas-pending"),
	))

	_, snapshot, err := s.repo.AdjustTerm(
		s.ctx,
		sub.ID,
		-1,
		true,
		true,
		now,
		service.MaxExpiresAt,
		service.MaxValidityDays,
	)
	s.Require().NoError(err)
	s.Require().NotNil(snapshot)
	s.Require().NotNil(snapshot.Expected)

	advanceAt := boundary.Add(time.Second)
	advanced, err := s.repo.AdvanceCycle(s.ctx, sub.ID, advanceAt, timezone.StartOfDay(advanceAt))
	s.Require().NoError(err)
	s.Require().True(advanced)

	err = s.repo.RestoreTermSnapshot(s.ctx, snapshot)
	s.Require().ErrorIs(err, subscriptionquota.ErrTermSnapshotStale)
	current, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(boundary, current.CurrentCycleStartsAt, time.Microsecond)
	s.Require().WithinDuration(nextEndsAt.AddDate(0, 0, -1), current.CurrentCycleEndsAt, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestAdjustTermAtomicallyCapturesAndRevokesThenRestores() {
	user := s.mustCreateUser("atomic-refund-revoke@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-atomic-refund-revoke")
	now := time.Now().UTC()
	startsAt := now.Add(-25 * 24 * time.Hour)
	endsAt := now.Add(5 * 24 * time.Hour)
	sub := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStartsAt(startsAt).
			SetExpiresAt(endsAt).
			SetCurrentCycleStartsAt(startsAt).
			SetCurrentCycleEndsAt(endsAt)
	}, &subscriptionCycleFixture{
		startsAt:   startsAt,
		endsAt:     endsAt,
		sourceType: subscriptionquota.CycleSourcePayment,
		sourceRef:  stringPointer("atomic-refund-order"),
	})

	subject, snapshot, err := s.repo.AdjustTerm(
		s.ctx,
		sub.ID,
		-10,
		true,
		true,
		now,
		service.MaxExpiresAt,
		service.MaxValidityDays,
	)
	s.Require().ErrorIs(err, service.ErrAdjustWouldExpire)
	s.Require().Equal(sub.ID, subject.ID)
	s.Require().NotNil(snapshot)
	s.Require().NotNil(snapshot.Expected)
	s.Require().NotNil(snapshot.Expected.DeletedAt)
	_, err = s.repo.GetByID(s.ctx, sub.ID)
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)

	restored, err := s.repo.RestoreTermSnapshotExact(s.ctx, snapshot)
	s.Require().NoError(err)
	s.Require().Equal(sub.ID, restored.ID)
	s.Require().Nil(restored.DeletedAt)
	s.Require().Len(snapshot.Cycles, 1)
	s.Require().Equal(subscriptionquota.CycleSourcePayment, snapshot.Cycles[0].SourceType)
	s.Require().Equal("atomic-refund-order", *snapshot.Cycles[0].SourceRef)
}

func stringPointer(value string) *string { return &value }

func findCycleSnapshotBySourceRef(snapshot *subscriptionquota.TermSnapshot, sourceRef string) (int, subscriptionquota.TermCycleSnapshot) {
	for index, cycle := range snapshot.Cycles {
		if cycle.SourceRef != nil && *cycle.SourceRef == sourceRef {
			return index, cycle
		}
	}
	return -1, subscriptionquota.TermCycleSnapshot{}
}

func (s *UserSubscriptionRepoSuite) TestResetWeeklyUsage() {
	user := s.mustCreateUser("resetw@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-resetw")
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetWeeklyUsageUsd(15.0)
		c.SetMonthlyUsageUsd(30.0)
	})

	resetAt := time.Date(2025, 1, 6, 0, 0, 0, 0, time.UTC)
	err := s.repo.ResetWeeklyUsage(s.ctx, sub.ID, sub.WeeklyWindowStart, resetAt)
	s.Require().NoError(err, "ResetWeeklyUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.0, got.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(30.0, got.MonthlyUsageUSD, 1e-6)
	s.Require().NotNil(got.WeeklyWindowStart)
	s.Require().WithinDuration(resetAt, *got.WeeklyWindowStart, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestResetMonthlyUsage() {
	user := s.mustCreateUser("resetm@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-resetm")
	sub := s.mustCreateSubscription(user.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetMonthlyUsageUsd(25.0)
	})

	resetAt := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	err := s.repo.ResetMonthlyUsage(s.ctx, sub.ID, sub.MonthlyWindowStart, resetAt)
	s.Require().NoError(err, "ResetMonthlyUsage")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().InDelta(0.0, got.MonthlyUsageUSD, 1e-6)
	s.Require().NotNil(got.MonthlyWindowStart)
	s.Require().WithinDuration(resetAt, *got.MonthlyWindowStart, time.Microsecond)
}

// --- UpdateStatus / ExtendExpiry / UpdateNotes ---

func (s *UserSubscriptionRepoSuite) TestUpdateStatus() {
	user := s.mustCreateUser("status@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-status")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	err := s.repo.UpdateStatus(s.ctx, sub.ID, service.SubscriptionStatusExpired)
	s.Require().NoError(err, "UpdateStatus")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Equal(service.SubscriptionStatusExpired, got.Status)
}

func (s *UserSubscriptionRepoSuite) TestExtendExpiry() {
	user := s.mustCreateUser("extend@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-extend")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	newExpiry := sub.ExpiresAt.Add(7 * 24 * time.Hour)
	err := s.repo.ExtendExpiry(s.ctx, sub.ID, newExpiry)
	s.Require().NoError(err, "ExtendExpiry")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().WithinDuration(newExpiry, got.ExpiresAt, time.Microsecond)
}

func (s *UserSubscriptionRepoSuite) TestUpdateNotes() {
	user := s.mustCreateUser("notes@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-notes")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	err := s.repo.UpdateNotes(s.ctx, sub.ID, "VIP user")
	s.Require().NoError(err, "UpdateNotes")

	got, err := s.repo.GetByID(s.ctx, sub.ID)
	s.Require().NoError(err)
	s.Require().Equal("VIP user", got.Notes)
}

// --- ListExpired / BatchUpdateExpiredStatus ---

func (s *UserSubscriptionRepoSuite) TestListExpired() {
	user := s.mustCreateUser("listexp@test.com", service.RoleUser)
	groupActive := s.mustCreateGroup("g-listexp-active")
	groupExpired := s.mustCreateGroup("g-listexp-expired")

	s.mustCreateSubscription(user.ID, groupActive.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user.ID, groupExpired.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	expired, err := s.repo.ListExpired(s.ctx)
	s.Require().NoError(err, "ListExpired")
	s.Require().Len(expired, 1)
}

func (s *UserSubscriptionRepoSuite) TestBatchUpdateExpiredStatus() {
	user := s.mustCreateUser("batch@test.com", service.RoleUser)
	groupFuture := s.mustCreateGroup("g-batch-future")
	groupPast := s.mustCreateGroup("g-batch-past")

	active := s.mustCreateSubscription(user.ID, groupFuture.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	expiredActive := s.mustCreateSubscription(user.ID, groupPast.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	affected, err := s.repo.BatchUpdateExpiredStatus(s.ctx)
	s.Require().NoError(err, "BatchUpdateExpiredStatus")
	s.Require().Equal(int64(1), affected)

	gotActive, _ := s.repo.GetByID(s.ctx, active.ID)
	s.Require().Equal(service.SubscriptionStatusActive, gotActive.Status)

	gotExpired, _ := s.repo.GetByID(s.ctx, expiredActive.ID)
	s.Require().Equal(service.SubscriptionStatusExpired, gotExpired.Status)
}

// --- ExistsByUserIDAndGroupID ---

func (s *UserSubscriptionRepoSuite) TestExistsByUserIDAndGroupID() {
	user := s.mustCreateUser("exists@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-exists")

	s.mustCreateSubscription(user.ID, group.ID, nil)

	exists, err := s.repo.ExistsByUserIDAndGroupID(s.ctx, user.ID, group.ID)
	s.Require().NoError(err, "ExistsByUserIDAndGroupID")
	s.Require().True(exists)

	notExists, err := s.repo.ExistsByUserIDAndGroupID(s.ctx, user.ID, 999999)
	s.Require().NoError(err)
	s.Require().False(notExists)
}

func (s *UserSubscriptionRepoSuite) TestExistsActiveByUserIDAndGroupID_IgnoresSoftDeletedRows() {
	user := s.mustCreateUser("exists-active@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-exists-active")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	exists, err := s.repo.ExistsActiveByUserIDAndGroupID(s.ctx, user.ID, group.ID)
	s.Require().NoError(err, "ExistsActiveByUserIDAndGroupID")
	s.Require().True(exists)

	s.Require().NoError(s.repo.Delete(s.ctx, sub.ID), "Delete")

	exists, err = s.repo.ExistsActiveByUserIDAndGroupID(s.ctx, user.ID, group.ID)
	s.Require().NoError(err, "ExistsActiveByUserIDAndGroupID after delete")
	s.Require().False(exists)
}

// --- CountByGroupID / CountActiveByGroupID ---

func (s *UserSubscriptionRepoSuite) TestCountByGroupID() {
	user1 := s.mustCreateUser("cnt1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("cnt2@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-count")

	s.mustCreateSubscription(user1.ID, group.ID, nil)
	s.mustCreateSubscription(user2.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetStatus(service.SubscriptionStatusExpired)
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour))
	})

	count, err := s.repo.CountByGroupID(s.ctx, group.ID)
	s.Require().NoError(err, "CountByGroupID")
	s.Require().Equal(int64(2), count)
}

func (s *UserSubscriptionRepoSuite) TestCountActiveByGroupID() {
	user1 := s.mustCreateUser("cntact1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("cntact2@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-cntact")

	s.mustCreateSubscription(user1.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(24 * time.Hour))
	})
	s.mustCreateSubscription(user2.ID, group.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-24 * time.Hour)) // expired by time
	})

	count, err := s.repo.CountActiveByGroupID(s.ctx, group.ID)
	s.Require().NoError(err, "CountActiveByGroupID")
	s.Require().Equal(int64(1), count, "only future expiry counts as active")
}

// --- DeleteByGroupID ---

func (s *UserSubscriptionRepoSuite) TestDeleteByGroupID() {
	user1 := s.mustCreateUser("delgrp1@test.com", service.RoleUser)
	user2 := s.mustCreateUser("delgrp2@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-delgrp")

	s.mustCreateSubscription(user1.ID, group.ID, nil)
	s.mustCreateSubscription(user2.ID, group.ID, nil)

	affected, err := s.repo.DeleteByGroupID(s.ctx, group.ID)
	s.Require().NoError(err, "DeleteByGroupID")
	s.Require().Equal(int64(2), affected)

	count, _ := s.repo.CountByGroupID(s.ctx, group.ID)
	s.Require().Zero(count)
}

// --- Combined scenario ---

func (s *UserSubscriptionRepoSuite) TestActiveExpiredBoundaries_UsageAndReset_BatchUpdateExpiredStatus() {
	user := s.mustCreateUser("subr@example.com", service.RoleUser)
	groupActive := s.mustCreateGroup("g-subr-active")
	groupExpired := s.mustCreateGroup("g-subr-expired")

	active := s.mustCreateSubscription(user.ID, groupActive.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(2 * time.Hour))
	})
	expiredActive := s.mustCreateSubscription(user.ID, groupExpired.ID, func(c *dbent.UserSubscriptionCreate) {
		c.SetExpiresAt(time.Now().Add(-2 * time.Hour))
	})

	got, err := s.repo.GetActiveByUserIDAndGroupID(s.ctx, user.ID, groupActive.ID)
	s.Require().NoError(err, "GetActiveByUserIDAndGroupID")
	s.Require().Equal(active.ID, got.ID, "expected active subscription")

	activateAt := time.Now().Add(-25 * time.Hour)
	s.Require().NoError(s.repo.ActivateWindows(s.ctx, active.ID, activateAt, activateAt), "ActivateWindows")
	s.Require().NoError(s.repo.IncrementUsage(s.ctx, active.ID, 1.25), "IncrementUsage")

	after, err := s.repo.GetByID(s.ctx, active.ID)
	s.Require().NoError(err, "GetByID")
	s.Require().InDelta(1.25, after.DailyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, after.WeeklyUsageUSD, 1e-6)
	s.Require().InDelta(1.25, after.MonthlyUsageUSD, 1e-6)
	s.Require().NotNil(after.DailyWindowStart, "expected DailyWindowStart activated")
	s.Require().NotNil(after.WeeklyWindowStart, "expected WeeklyWindowStart activated")
	s.Require().NotNil(after.MonthlyWindowStart, "expected MonthlyWindowStart activated")

	resetAt := time.Now().Truncate(time.Microsecond) // truncate to microsecond for DB precision
	s.Require().NoError(s.repo.ResetDailyUsage(s.ctx, active.ID, after.DailyWindowStart, resetAt), "ResetDailyUsage")
	afterReset, err := s.repo.GetByID(s.ctx, active.ID)
	s.Require().NoError(err, "GetByID after reset")
	s.Require().InDelta(0.0, afterReset.DailyUsageUSD, 1e-6)
	s.Require().NotNil(afterReset.DailyWindowStart)
	s.Require().WithinDuration(resetAt, *afterReset.DailyWindowStart, time.Microsecond)

	affected, err := s.repo.BatchUpdateExpiredStatus(s.ctx)
	s.Require().NoError(err, "BatchUpdateExpiredStatus")
	s.Require().Equal(int64(1), affected, "expected 1 affected row")

	updated, err := s.repo.GetByID(s.ctx, expiredActive.ID)
	s.Require().NoError(err, "GetByID expired")
	s.Require().Equal(service.SubscriptionStatusExpired, updated.Status, "expected status expired")
}

// --- 软删除过滤测试 ---

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_SoftDeletedGroup() {
	user := s.mustCreateUser("softdeleted@test.com", service.RoleUser)
	group := s.mustCreateGroup("g-softdeleted")
	sub := s.mustCreateSubscription(user.ID, group.ID, nil)

	// 软删除分组
	_, err := s.client.Group.UpdateOneID(group.ID).SetDeletedAt(time.Now()).Save(s.ctx)
	s.Require().NoError(err, "soft delete group")

	// IncrementUsage 应该失败，因为分组已软删除
	err = s.repo.IncrementUsage(s.ctx, sub.ID, 1.0)
	s.Require().Error(err, "should fail for soft-deleted group")
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)
}

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_NotFound() {
	err := s.repo.IncrementUsage(s.ctx, 999999, 1.0)
	s.Require().Error(err, "should fail for non-existent subscription")
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)
}

// --- nil 入参测试 ---

func (s *UserSubscriptionRepoSuite) TestCreate_NilInput() {
	err := s.repo.Create(s.ctx, nil)
	s.Require().Error(err, "Create should fail with nil input")
	s.Require().ErrorIs(err, service.ErrSubscriptionNilInput)
}

func (s *UserSubscriptionRepoSuite) TestUpdate_NilInput() {
	err := s.repo.Update(s.ctx, nil)
	s.Require().Error(err, "Update should fail with nil input")
	s.Require().ErrorIs(err, service.ErrSubscriptionNilInput)
}

// --- 并发用量更新测试 ---

func (s *UserSubscriptionRepoSuite) TestIncrementUsage_Concurrent() {
	ctx := context.Background()
	client := testEntClient(s.T())
	repo := subscriptionrepository.New(client, NewUserSubscriptionRepository(client))
	user := mustCreateUser(s.T(), client, &service.User{
		Email: fmt.Sprintf("subscription-concurrent-%d@test.com", time.Now().UnixNano()),
		Role:  service.RoleUser,
	})
	group := mustCreateGroup(s.T(), client, &service.Group{
		Name: fmt.Sprintf("subscription-concurrent-%d", time.Now().UnixNano()),
	})
	sub := mustCreateSubscription(s.T(), client, &service.UserSubscription{
		UserID:  user.ID,
		GroupID: group.ID,
	})
	t := s.T()
	t.Cleanup(func() {
		cleanupCtx := mixins.SkipSoftDelete(context.Background())
		client.UserSubscription.DeleteOneID(sub.ID).ExecX(cleanupCtx)
		client.Group.DeleteOneID(group.ID).ExecX(cleanupCtx)
		client.User.DeleteOneID(user.ID).ExecX(cleanupCtx)
	})

	const numGoroutines = 10
	const incrementPerGoroutine = 1.5

	// 启动多个 goroutine 并发调用 IncrementUsage
	errCh := make(chan error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			errCh <- repo.IncrementUsage(ctx, sub.ID, incrementPerGoroutine)
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		err := <-errCh
		s.Require().NoError(err, "IncrementUsage should succeed")
	}

	// 验证累加结果正确
	got, err := repo.GetByID(ctx, sub.ID)
	s.Require().NoError(err)
	expectedUsage := float64(numGoroutines) * incrementPerGoroutine
	s.Require().InDelta(expectedUsage, got.DailyUsageUSD, 1e-6, "daily usage should be correctly accumulated")
	s.Require().InDelta(expectedUsage, got.WeeklyUsageUSD, 1e-6, "weekly usage should be correctly accumulated")
	s.Require().InDelta(expectedUsage, got.MonthlyUsageUSD, 1e-6, "monthly usage should be correctly accumulated")
}

func (s *UserSubscriptionRepoSuite) TestTxContext_RollbackIsolation() {
	baseClient := testEntClient(s.T())
	tx, err := baseClient.Tx(context.Background())
	s.Require().NoError(err, "begin tx")
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	txCtx := dbent.NewTxContext(context.Background(), tx)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	userEnt, err := tx.Client().User.Create().
		SetEmail("tx-user-" + suffix + "@example.com").
		SetPasswordHash("test").
		Save(txCtx)
	s.Require().NoError(err, "create user in tx")

	groupEnt, err := tx.Client().Group.Create().
		SetName("tx-group-" + suffix).
		Save(txCtx)
	s.Require().NoError(err, "create group in tx")

	repo := NewUserSubscriptionRepository(baseClient)
	sub := &service.UserSubscription{
		UserID:     userEnt.ID,
		GroupID:    groupEnt.ID,
		ExpiresAt:  time.Now().AddDate(0, 0, 30),
		Status:     service.SubscriptionStatusActive,
		AssignedAt: time.Now(),
		Notes:      "tx",
	}
	s.Require().NoError(repo.Create(txCtx, sub), "create subscription in tx")
	s.Require().NoError(repo.UpdateNotes(txCtx, sub.ID, "tx-note"), "update subscription in tx")

	s.Require().NoError(tx.Rollback(), "rollback tx")
	tx = nil

	_, err = repo.GetByID(context.Background(), sub.ID)
	s.Require().ErrorIs(err, service.ErrSubscriptionNotFound)
}

func (s *UserSubscriptionRepoSuite) TestManualBulkResetEligibilityUpdateAndMetadata() {
	now := time.Now().UTC()
	user := s.mustCreateUser("manual-bulk-reset@example.com", service.RoleUser)
	group := s.mustCreateGroup("manual-bulk-reset-group")
	subscription := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, func(create *dbent.UserSubscriptionCreate) {
		create.
			SetDailyUsageUsd(1.25).
			SetWeeklyUsageUsd(2.5).
			SetMonthlyUsageUsd(3.75).
			SetCycleUsageUsd(4.5).
			SetManualQuotaResetCount(2)
	}, &subscriptionCycleFixture{
		startsAt:   now.Add(-time.Hour),
		endsAt:     now.Add(24 * time.Hour),
		sourceType: subscriptionquota.CycleSourceAssignment,
	})
	before, err := s.repo.GetByID(s.ctx, subscription.ID)
	s.Require().NoError(err)

	eligibilityService := subscriptionbulkreset.NewService(s.client, nil)
	s.Require().NoError(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, subscription.ID, true))
	s.Require().NoError(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, subscription.ID, true), "setting the same value must be idempotent")
	after, err := s.repo.GetByID(s.ctx, subscription.ID)
	s.Require().NoError(err)
	s.Equal(before.StartsAt, after.StartsAt)
	s.Equal(before.ExpiresAt, after.ExpiresAt)
	s.Equal(before.DailyUsageUSD, after.DailyUsageUSD)
	s.Equal(before.WeeklyUsageUSD, after.WeeklyUsageUSD)
	s.Equal(before.MonthlyUsageUSD, after.MonthlyUsageUSD)
	s.Equal(before.CycleUsageUSD, after.CycleUsageUSD)
	s.Equal(before.ManualQuotaResetCount, after.ManualQuotaResetCount)

	adminView, err := s.repo.GetByID(s.ctx, subscription.ID)
	s.Require().NoError(err)
	s.Equal(subscriptionquota.CycleSourceAssignment, adminView.CycleSourceType)
	s.True(adminView.ManualBulkQuotaResetEnabled)
	s.True(adminView.ManualBulkQuotaResetEditable)
	candidates, err := eligibilityService.ListCandidates(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(candidates.Items, 1)
	s.Equal(subscription.ID, candidates.Items[0].SubscriptionID)
	s.Nil(candidates.Items[0].PlanID)
	s.Equal(group.Name, candidates.Items[0].GroupName)

	s.Require().NoError(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, subscription.ID, false))
	candidates, err = eligibilityService.ListCandidates(s.ctx)
	s.Require().NoError(err)
	s.Empty(candidates.Items)
}

func (s *UserSubscriptionRepoSuite) TestManualBulkResetEligibilityRejectsPaymentRedeemExpiredAndRevoked() {
	now := time.Now().UTC()
	user := s.mustCreateUser("manual-bulk-reset-reject@example.com", service.RoleUser)
	eligibilityService := subscriptionbulkreset.NewService(s.client, nil)

	for index, sourceType := range []string{subscriptionquota.CycleSourcePayment, subscriptionquota.CycleSourceRedeem} {
		group := s.mustCreateGroup(fmt.Sprintf("manual-bulk-reset-reject-%d", index))
		subscription := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, nil, &subscriptionCycleFixture{
			startsAt:   now.Add(-time.Hour),
			endsAt:     now.Add(24 * time.Hour),
			sourceType: sourceType,
		})
		s.Error(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, subscription.ID, true))
		adminView, err := s.repo.GetByID(s.ctx, subscription.ID)
		s.Require().NoError(err)
		s.False(adminView.ManualBulkQuotaResetEnabled, "payment and redeem eligibility must be ignored")
		s.False(adminView.ManualBulkQuotaResetEditable)
	}

	expiredGroup := s.mustCreateGroup("manual-bulk-reset-expired")
	expired := s.mustCreateSubscriptionWithCycle(user.ID, expiredGroup.ID, func(create *dbent.UserSubscriptionCreate) {
		create.SetExpiresAt(now.Add(-time.Minute))
	}, &subscriptionCycleFixture{
		startsAt:   now.Add(-48 * time.Hour),
		endsAt:     now.Add(24 * time.Hour),
		sourceType: subscriptionquota.CycleSourceAssignment,
	})
	s.Error(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, expired.ID, true))

	revokedGroup := s.mustCreateGroup("manual-bulk-reset-revoked")
	revoked := s.mustCreateSubscriptionWithCycle(user.ID, revokedGroup.ID, nil, &subscriptionCycleFixture{
		startsAt:   now.Add(-time.Hour),
		endsAt:     now.Add(24 * time.Hour),
		sourceType: subscriptionquota.CycleSourceLegacy,
	})
	s.Require().NoError(s.repo.Delete(s.ctx, revoked.ID))
	s.Error(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, revoked.ID, true))
}

func (s *UserSubscriptionRepoSuite) TestManualBulkResetEligibilityAdvancesBeforeUpdatingBoundaryCycle() {
	now := time.Now().UTC()
	boundary := now.Add(-time.Second)
	user := s.mustCreateUser("manual-bulk-reset-boundary@example.com", service.RoleUser)
	group := s.mustCreateGroup("manual-bulk-reset-boundary")
	subscription := s.mustCreateSubscriptionWithCycle(user.ID, group.ID, nil, &subscriptionCycleFixture{
		startsAt:   boundary.Add(-24 * time.Hour),
		endsAt:     boundary,
		sourceType: subscriptionquota.CycleSourceAssignment,
	})
	s.Require().NoError(s.repo.AppendRenewalCycle(
		s.ctx,
		subscription.ID,
		boundary,
		boundary.Add(24*time.Hour),
		subscriptionquota.CycleSourceLegacy,
		nil,
	))

	eligibilityService := subscriptionbulkreset.NewService(s.client, nil)
	s.Require().NoError(eligibilityService.UpdateCurrentCycleManualEligibility(s.ctx, subscription.ID, true))
	adminView, err := s.repo.GetByID(s.ctx, subscription.ID)
	s.Require().NoError(err)
	s.Equal(subscriptionquota.CycleSourceLegacy, adminView.CycleSourceType)
	s.True(adminView.ManualBulkQuotaResetEnabled)
	s.True(adminView.ManualBulkQuotaResetEditable)
}
