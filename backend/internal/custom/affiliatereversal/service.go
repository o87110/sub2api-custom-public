package affiliatereversal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/useraffiliate"
	"github.com/Wei-Shaw/sub2api/ent/useraffiliateledger"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type Service struct {
	client               *dbent.Client
	authCacheInvalidator service.APIKeyAuthCacheInvalidator
	billingCacheService  balanceCacheInvalidator
}

type balanceCacheInvalidator interface {
	InvalidateUserBalance(context.Context, int64) error
}

const cacheInvalidationTimeout = 5 * time.Second

func NewService(client *dbent.Client, authCacheInvalidator service.APIKeyAuthCacheInvalidator, billingCacheService *service.BillingCacheService) *Service {
	var balanceCache balanceCacheInvalidator
	if billingCacheService != nil {
		balanceCache = billingCacheService
	}
	return &Service{
		client:               client,
		authCacheInvalidator: authCacheInvalidator,
		billingCacheService:  balanceCache,
	}
}

func NormalizeOrderIDs(orderIDs []int64) ([]int64, error) {
	if len(orderIDs) == 0 || len(orderIDs) > MaxBatchSize {
		return nil, ErrInvalidOrders
	}
	seen := make(map[int64]struct{}, len(orderIDs))
	normalized := make([]int64, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		if orderID <= 0 {
			return nil, ErrInvalidOrders
		}
		if _, ok := seen[orderID]; ok {
			continue
		}
		seen[orderID] = struct{}{}
		normalized = append(normalized, orderID)
	}
	if len(normalized) == 0 || len(normalized) > MaxBatchSize {
		return nil, ErrInvalidOrders
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
}

func NormalizeReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > MaxReasonLen {
		return "", ErrInvalidReason
	}
	return reason, nil
}

func NormalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusActive:
		return StatusActive
	case StatusReversed:
		return StatusReversed
	default:
		return StatusAll
	}
}

func (s *Service) Preview(ctx context.Context, orderIDs []int64) (*Preview, error) {
	ids, err := NormalizeOrderIDs(orderIDs)
	if err != nil {
		return nil, err
	}
	targets, accounts, reversedCount, err := loadTargetSnapshots(ctx, s.client, ids)
	if err != nil {
		return nil, err
	}
	if reversedCount > 0 || len(targets) != len(ids) {
		return nil, ErrTargetInvalid
	}
	plan, err := calculatePlan(targets, accounts)
	if err != nil {
		return nil, err
	}
	token, err := previewToken(targets, accounts)
	if err != nil {
		return nil, err
	}
	plan.Preview.PreviewToken = token
	return &plan.Preview, nil
}

func (s *Service) Reverse(ctx context.Context, input ReverseInput, operatorUserID int64, execution idempotencyexecution.Execution) (_ *ReverseResult, err error) {
	ids, err := NormalizeOrderIDs(input.OrderIDs)
	if err != nil {
		return nil, err
	}
	reason, err := NormalizeReason(input.Reason)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.PreviewToken) == "" || strings.TrimSpace(execution.OperationKeyHash) == "" {
		return nil, ErrPreviewStale
	}
	if recovered, recoverErr := s.recoverResult(ctx, ids, execution.OperationKeyHash); recoverErr != nil || recovered != nil {
		if recoverErr == nil && recovered != nil {
			s.invalidateCachesForInviters(reversalInviterIDs(recovered))
		}
		return recovered, recoverErr
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin affiliate reversal transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	txCtx := dbent.NewTxContext(ctx, tx)
	txClient := tx.Client()

	if err = lockLedgerRows(txCtx, txClient, ids); err != nil {
		return nil, err
	}
	var accounts map[int64]accountSnapshot
	targets, _, reversedCount, err := loadTargetSnapshots(txCtx, txClient, ids)
	if err != nil {
		return nil, err
	}
	if reversedCount > 0 || len(targets) != len(ids) {
		return nil, ErrTargetInvalid
	}
	inviterIDs := sortedInviterIDs(targets)
	if err = lockAffiliateAccounts(txCtx, txClient, inviterIDs); err != nil {
		return nil, err
	}
	if err = lockUsers(txCtx, txClient, inviterIDs); err != nil {
		return nil, err
	}

	// Re-read every mutable snapshot after all deterministic row locks are held.
	targets, accounts, reversedCount, err = loadTargetSnapshots(txCtx, txClient, ids)
	if err != nil {
		return nil, err
	}
	if reversedCount > 0 || len(targets) != len(ids) {
		return nil, ErrTargetInvalid
	}
	liveToken, err := previewToken(targets, accounts)
	if err != nil {
		return nil, err
	}
	if liveToken != strings.TrimSpace(input.PreviewToken) {
		return nil, ErrPreviewStale
	}
	plan, err := calculatePlan(targets, accounts)
	if err != nil {
		return nil, err
	}
	if plan.Preview.HasNegativeBalance && !input.ConfirmNegativeBalance {
		return nil, ErrNegativeConfirmation
	}

	for _, inviterID := range inviterIDs {
		final := plan.FinalAccounts[inviterID]
		if err = updateAffiliateAccount(txCtx, txClient, final); err != nil {
			return nil, err
		}
		if err = updateUserFinancials(txCtx, txClient, final); err != nil {
			return nil, err
		}
	}

	result := &ReverseResult{
		ReversedCount:        len(plan.Orders),
		TotalRebateAmount:    plan.Preview.TotalRebateAmount,
		TotalBalanceDeducted: plan.Preview.TotalBalanceDeducted,
		NegativeBalanceUsers: plan.Preview.NegativeBalanceUsers,
		Orders:               make([]ReverseOrderResult, 0, len(plan.Orders)),
		Inviters:             append([]InviterImpact(nil), plan.Preview.Inviters...),
	}
	for _, item := range plan.Orders {
		if err = markLedgerReversed(txCtx, txClient, item); err != nil {
			return nil, err
		}
		reversalID, insertErr := insertReversal(txCtx, txClient, item, reason, operatorUserID, execution.OperationKeyHash)
		if insertErr != nil {
			return nil, insertErr
		}
		if insertErr = insertPaymentAudit(txCtx, txClient, item, reversalID, reason, operatorUserID, execution.OperationKeyHash); insertErr != nil {
			return nil, insertErr
		}
		result.Orders = append(result.Orders, ReverseOrderResult{
			ReversalID:             reversalID,
			OrderID:                item.Target.OrderID,
			LedgerID:               item.Target.LedgerID,
			InviterID:              item.Target.InviterID,
			InviteeID:              item.Target.InviteeID,
			RebateAmount:           item.Target.Amount,
			FrozenQuotaDeducted:    item.FrozenDeducted,
			AvailableQuotaDeducted: item.AvailableDeducted,
			BalanceDeducted:        item.BalanceDeducted,
		})
	}
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit affiliate reversal transaction: %w", err)
	}

	s.invalidateCachesForInviters(inviterIDs)
	return result, nil
}

// InvalidateCachesForOrders repairs cache state for a successful idempotency
// replay. The generic coordinator can return a persisted response without
// entering Service.Reverse, so the inviter IDs must be resolved from the
// committed reversal rows instead of relying on the response payload type.
func (s *Service) InvalidateCachesForOrders(orderIDs []int64) {
	if s == nil || s.client == nil {
		return
	}
	ids, err := NormalizeOrderIDs(orderIDs)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cacheInvalidationTimeout)
	defer cancel()
	inviterIDs, err := loadReversalInviterIDs(ctx, s.client, ids)
	if err != nil {
		logger.LegacyPrintf("custom.affiliate_reversal", "load inviter IDs for replay cache invalidation failed: %v", err)
		return
	}
	s.invalidateCachesWithContext(ctx, inviterIDs)
}

func (s *Service) invalidateCachesForInviters(inviterIDs []int64) {
	ctx, cancel := context.WithTimeout(context.Background(), cacheInvalidationTimeout)
	defer cancel()
	s.invalidateCachesWithContext(ctx, inviterIDs)
}

func (s *Service) invalidateCachesWithContext(ctx context.Context, inviterIDs []int64) {
	if s == nil {
		return
	}
	unique := make(map[int64]struct{}, len(inviterIDs))
	ordered := make([]int64, 0, len(inviterIDs))
	for _, inviterID := range inviterIDs {
		if inviterID <= 0 {
			continue
		}
		if _, exists := unique[inviterID]; exists {
			continue
		}
		unique[inviterID] = struct{}{}
		ordered = append(ordered, inviterID)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, inviterID := range ordered {
		if s.authCacheInvalidator != nil {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, inviterID)
		}
		if s.billingCacheService != nil {
			if cacheErr := s.billingCacheService.InvalidateUserBalance(ctx, inviterID); cacheErr != nil {
				logger.LegacyPrintf("custom.affiliate_reversal", "invalidate balance cache failed for user %d: %v", inviterID, cacheErr)
			}
		}
	}
}

func reversalInviterIDs(result *ReverseResult) []int64 {
	if result == nil {
		return nil
	}
	ids := make([]int64, 0, len(result.Inviters))
	for _, inviter := range result.Inviters {
		ids = append(ids, inviter.InviterID)
	}
	return ids
}

type queryExecer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadReversalInviterIDs(ctx context.Context, client queryExecer, orderIDs []int64) ([]int64, error) {
	if client == nil || len(orderIDs) == 0 {
		return nil, nil
	}
	rows, err := client.QueryContext(ctx, `
SELECT DISTINCT inviter_user_id
FROM user_affiliate_reversals
WHERE source_order_id = ANY($1)
ORDER BY inviter_user_id`, pq.Array(orderIDs))
	if err != nil {
		return nil, fmt.Errorf("query affiliate reversal inviters: %w", err)
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0, len(orderIDs))
	for rows.Next() {
		var inviterID int64
		if err := rows.Scan(&inviterID); err != nil {
			return nil, err
		}
		ids = append(ids, inviterID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func loadTargetSnapshots(ctx context.Context, client queryExecer, ids []int64) ([]targetSnapshot, map[int64]accountSnapshot, int, error) {
	rows, err := client.QueryContext(ctx, `
SELECT ual.id,
       ual.source_order_id,
       ual.user_id,
       ual.source_user_id,
       ual.amount::double precision,
       ual.frozen_until,
       ual.created_at,
       ual.updated_at,
       COALESCE(inviter.email, ''),
       COALESCE(inviter.username, ''),
       ua.aff_quota::double precision,
       ua.aff_frozen_quota::double precision,
       ua.aff_history_quota::double precision,
       inviter.balance::double precision,
       inviter.total_recharged::double precision,
       ua.updated_at,
       inviter.updated_at,
       reversal.id
FROM user_affiliate_ledger ual
JOIN user_affiliates ua ON ua.user_id = ual.user_id
JOIN users inviter ON inviter.id = ual.user_id
LEFT JOIN user_affiliate_reversals reversal ON reversal.source_order_id = ual.source_order_id
WHERE ual.action = 'accrue'
  AND ual.source_order_id = ANY($1)
ORDER BY ual.user_id, ual.created_at, ual.id`, pq.Array(ids))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("query affiliate reversal targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	targets := make([]targetSnapshot, 0, len(ids))
	accounts := make(map[int64]accountSnapshot)
	reversedCount := 0
	seenOrders := make(map[int64]struct{}, len(ids))
	for rows.Next() {
		var target targetSnapshot
		var frozenUntil sql.NullTime
		var reversalID sql.NullInt64
		var account accountSnapshot
		if err := rows.Scan(
			&target.LedgerID,
			&target.OrderID,
			&target.InviterID,
			&target.InviteeID,
			&target.Amount,
			&frozenUntil,
			&target.CreatedAt,
			&target.LedgerUpdatedAt,
			&account.Email,
			&account.Username,
			&account.AffQuota,
			&account.AffFrozenQuota,
			&account.AffHistoryQuota,
			&account.Balance,
			&account.TotalRecharged,
			&account.AffiliateUpdatedAt,
			&account.UserUpdatedAt,
			&reversalID,
		); err != nil {
			return nil, nil, 0, err
		}
		if _, duplicate := seenOrders[target.OrderID]; duplicate {
			return nil, nil, 0, ErrTargetInvalid
		}
		seenOrders[target.OrderID] = struct{}{}
		if frozenUntil.Valid {
			value := frozenUntil.Time
			target.FrozenUntil = &value
		}
		if reversalID.Valid {
			reversedCount++
		}
		account.InviterID = target.InviterID
		if existing, ok := accounts[target.InviterID]; ok {
			if existing.AffiliateUpdatedAt != account.AffiliateUpdatedAt || existing.UserUpdatedAt != account.UserUpdatedAt {
				return nil, nil, 0, ErrInvariant
			}
		} else {
			accounts[target.InviterID] = account
		}
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	return targets, accounts, reversedCount, nil
}

func sortedInviterIDs(targets []targetSnapshot) []int64 {
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, target := range targets {
		if _, ok := seen[target.InviterID]; ok {
			continue
		}
		seen[target.InviterID] = struct{}{}
		ids = append(ids, target.InviterID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func lockLedgerRows(ctx context.Context, client *dbent.Client, orderIDs []int64) error {
	_, err := client.UserAffiliateLedger.Query().
		Where(
			useraffiliateledger.ActionEQ("accrue"),
			useraffiliateledger.SourceOrderIDIn(orderIDs...),
		).
		Order(dbent.Asc(useraffiliateledger.FieldID)).
		ForUpdate().
		IDs(ctx)
	return err
}

func lockAffiliateAccounts(ctx context.Context, client *dbent.Client, userIDs []int64) error {
	_, err := client.UserAffiliate.Query().
		Where(useraffiliate.IDIn(userIDs...)).
		Order(dbent.Asc(useraffiliate.FieldID)).
		ForUpdate().
		IDs(ctx)
	return err
}

func lockUsers(ctx context.Context, client *dbent.Client, userIDs []int64) error {
	_, err := client.User.Query().
		Where(user.IDIn(userIDs...), user.DeletedAtIsNil()).
		Order(dbent.Asc(user.FieldID)).
		ForUpdate().
		IDs(ctx)
	return err
}

func updateAffiliateAccount(ctx context.Context, client *dbent.Client, account accountSnapshot) error {
	_, err := client.UserAffiliate.UpdateOneID(account.InviterID).
		SetAffQuota(account.AffQuota).
		SetAffFrozenQuota(account.AffFrozenQuota).
		SetAffHistoryQuota(account.AffHistoryQuota).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("update affiliate reversal quota: %w", err)
	}
	return nil
}

func updateUserFinancials(ctx context.Context, client *dbent.Client, account accountSnapshot) error {
	_, err := client.User.UpdateOneID(account.InviterID).
		SetBalance(account.Balance).
		SetTotalRecharged(account.TotalRecharged).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("update affiliate reversal balance: %w", err)
	}
	return nil
}

func markLedgerReversed(ctx context.Context, client *dbent.Client, item orderPlan) error {
	_, err := client.UserAffiliateLedger.UpdateOneID(item.Target.LedgerID).
		Where(
			useraffiliateledger.ActionEQ("accrue"),
			useraffiliateledger.SourceOrderIDEQ(item.Target.OrderID),
		).
		SetAction("reversed").
		ClearFrozenUntil().
		Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return ErrTargetInvalid
		}
		return fmt.Errorf("mark affiliate ledger reversed: %w", err)
	}
	return nil
}

func insertReversal(ctx context.Context, client *dbent.Client, item orderPlan, reason string, operatorUserID int64, operationKeyHash string) (int64, error) {
	create := client.UserAffiliateReversal.Create().
		SetSourceLedgerID(item.Target.LedgerID).
		SetSourceOrderID(item.Target.OrderID).
		SetInviterUserID(item.Target.InviterID).
		SetInviteeUserID(item.Target.InviteeID).
		SetRebateAmount(item.Target.Amount).
		SetFrozenQuotaDeducted(item.FrozenDeducted).
		SetAvailableQuotaDeducted(item.AvailableDeducted).
		SetBalanceDeducted(item.BalanceDeducted).
		SetTotalRechargedDeducted(item.BalanceDeducted).
		SetBalanceBefore(item.BalanceBefore).
		SetBalanceAfter(item.BalanceAfter).
		SetAffQuotaBefore(item.AffQuotaBefore).
		SetAffQuotaAfter(item.AffQuotaAfter).
		SetAffFrozenQuotaBefore(item.AffFrozenBefore).
		SetAffFrozenQuotaAfter(item.AffFrozenAfter).
		SetAffHistoryQuotaBefore(item.AffHistoryBefore).
		SetAffHistoryQuotaAfter(item.AffHistoryAfter).
		SetTotalRechargedBefore(item.TotalRechargedBefore).
		SetTotalRechargedAfter(item.TotalRechargedAfter).
		SetSnapshotAvailable(true).
		SetReason(reason).
		SetOperationKeyHash(operationKeyHash)
	if operatorUserID > 0 {
		create = create.SetOperatorUserID(operatorUserID)
	}
	reversal, err := create.Save(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			return 0, ErrTargetInvalid
		}
		return 0, fmt.Errorf("insert affiliate reversal: %w", err)
	}
	return reversal.ID, nil
}

func insertPaymentAudit(ctx context.Context, client *dbent.Client, item orderPlan, reversalID int64, reason string, operatorUserID int64, operationKeyHash string) error {
	detail, err := json.Marshal(map[string]any{
		"reversalId":             reversalID,
		"sourceLedgerId":         item.Target.LedgerID,
		"rebateAmount":           item.Target.Amount,
		"frozenQuotaDeducted":    item.FrozenDeducted,
		"availableQuotaDeducted": item.AvailableDeducted,
		"balanceDeducted":        item.BalanceDeducted,
		"balanceBefore":          item.BalanceBefore,
		"balanceAfter":           item.BalanceAfter,
		"reason":                 reason,
		"operatorUserId":         operatorUserID,
		"operationKeyHash":       operationKeyHash,
	})
	if err != nil {
		return err
	}
	_, err = client.PaymentAuditLog.Create().
		SetOrderID(fmt.Sprint(item.Target.OrderID)).
		SetAction("AFFILIATE_REBATE_REVERSED").
		SetDetail(string(detail)).
		SetOperator("admin").
		Save(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			return ErrTargetInvalid
		}
		return fmt.Errorf("insert affiliate reversal payment audit: %w", err)
	}
	return nil
}

type recoveredReversalRow struct {
	order                       ReverseOrderResult
	balanceBefore, balanceAfter float64
	historyBefore, historyAfter float64
	totalBefore, totalAfter     float64
	operationHash               string
}

func (s *Service) recoverResult(ctx context.Context, orderIDs []int64, operationKeyHash string) (*ReverseResult, error) {
	rows, err := s.client.QueryContext(ctx, `
SELECT id, source_ledger_id, source_order_id, inviter_user_id, invitee_user_id,
       rebate_amount::double precision,
       COALESCE(frozen_quota_deducted, 0)::double precision,
       COALESCE(available_quota_deducted, 0)::double precision,
       COALESCE(balance_deducted, 0)::double precision,
       COALESCE(balance_before, 0)::double precision,
       COALESCE(balance_after, 0)::double precision,
       COALESCE(aff_history_quota_before, 0)::double precision,
       COALESCE(aff_history_quota_after, 0)::double precision,
       COALESCE(total_recharged_before, 0)::double precision,
       COALESCE(total_recharged_after, 0)::double precision,
       COALESCE(operation_key_hash, '')
FROM user_affiliate_reversals
WHERE source_order_id = ANY($1)
ORDER BY id`, pq.Array(orderIDs))
	if err != nil {
		return nil, fmt.Errorf("recover affiliate reversal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	recovered := make([]recoveredReversalRow, 0, len(orderIDs))
	for rows.Next() {
		var row recoveredReversalRow
		var ledgerID sql.NullInt64
		if err := rows.Scan(
			&row.order.ReversalID,
			&ledgerID,
			&row.order.OrderID,
			&row.order.InviterID,
			&row.order.InviteeID,
			&row.order.RebateAmount,
			&row.order.FrozenQuotaDeducted,
			&row.order.AvailableQuotaDeducted,
			&row.order.BalanceDeducted,
			&row.balanceBefore,
			&row.balanceAfter,
			&row.historyBefore,
			&row.historyAfter,
			&row.totalBefore,
			&row.totalAfter,
			&row.operationHash,
		); err != nil {
			return nil, err
		}
		if ledgerID.Valid {
			row.order.LedgerID = ledgerID.Int64
		}
		recovered = append(recovered, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(recovered) == 0 {
		return nil, nil
	}
	if len(recovered) != len(orderIDs) {
		return nil, ErrTargetInvalid
	}
	for _, row := range recovered {
		if row.operationHash != operationKeyHash {
			return nil, ErrTargetInvalid
		}
	}
	return buildRecoveredResult(recovered), nil
}

func buildRecoveredResult(rows []recoveredReversalRow) *ReverseResult {
	result := &ReverseResult{Orders: make([]ReverseOrderResult, 0, len(rows))}
	impacts := make(map[int64]*InviterImpact)
	for _, row := range rows {
		result.Orders = append(result.Orders, row.order)
		result.TotalRebateAmount = roundMoney(result.TotalRebateAmount + row.order.RebateAmount)
		result.TotalBalanceDeducted = roundMoney(result.TotalBalanceDeducted + row.order.BalanceDeducted)
		impact := impacts[row.order.InviterID]
		if impact == nil {
			impact = &InviterImpact{
				InviterID:            row.order.InviterID,
				BalanceBefore:        row.balanceBefore,
				HistoryQuotaBefore:   row.historyBefore,
				TotalRechargedBefore: row.totalBefore,
			}
			impacts[row.order.InviterID] = impact
		}
		impact.OrderCount++
		impact.TotalRebateAmount = roundMoney(impact.TotalRebateAmount + row.order.RebateAmount)
		impact.FrozenQuotaDeducted = roundMoney(impact.FrozenQuotaDeducted + row.order.FrozenQuotaDeducted)
		impact.AvailableQuotaDeducted = roundMoney(impact.AvailableQuotaDeducted + row.order.AvailableQuotaDeducted)
		impact.BalanceDeducted = roundMoney(impact.BalanceDeducted + row.order.BalanceDeducted)
		impact.BalanceAfter = row.balanceAfter
		impact.HistoryQuotaAfter = row.historyAfter
		impact.TotalRechargedAfter = row.totalAfter
	}
	ids := make([]int64, 0, len(impacts))
	for id := range impacts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		impact := impacts[id]
		impact.WillBeNegative = impact.BalanceAfter < -moneyEpsilon
		if impact.WillBeNegative {
			result.NegativeBalanceUsers++
		}
		result.Inviters = append(result.Inviters, *impact)
	}
	result.ReversedCount = len(result.Orders)
	return result
}
