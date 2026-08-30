package affiliatereversal

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const rebateRecordsCTE = `
WITH rebate_records AS (
    SELECT ual.id AS ledger_id,
           po.id AS order_id,
           po.out_trade_no,
           ual.user_id AS inviter_id,
           COALESCE(inviter.email, '') AS inviter_email,
           COALESCE(inviter.username, '') AS inviter_username,
           ual.source_user_id AS invitee_id,
           COALESCE(invitee.email, '') AS invitee_email,
           COALESCE(invitee.username, '') AS invitee_username,
           po.amount::double precision AS order_amount,
           po.pay_amount::double precision AS pay_amount,
           ual.amount::double precision AS rebate_amount,
           po.payment_type,
           po.status AS order_status,
           ual.created_at AS rebate_created_at,
           CASE WHEN reversal.id IS NULL THEN 'active' ELSE 'reversed' END AS rebate_status,
           reversal.created_at AS reversed_at,
           reversal.reason AS reversal_reason,
           reversal.operator_user_id AS reversed_by_user_id,
           COALESCE(reversal.snapshot_available, FALSE) AS snapshot_available,
           reversal.frozen_quota_deducted::double precision AS frozen_quota_deducted,
           reversal.available_quota_deducted::double precision AS available_quota_deducted,
           reversal.balance_deducted::double precision AS balance_deducted,
           reversal.balance_before::double precision AS balance_before,
           reversal.balance_after::double precision AS balance_after
    FROM user_affiliate_ledger ual
    JOIN payment_orders po ON po.id = ual.source_order_id
    JOIN users invitee ON invitee.id = ual.source_user_id
    JOIN users inviter ON inviter.id = ual.user_id
    LEFT JOIN user_affiliate_reversals reversal ON reversal.source_order_id = po.id
    WHERE ual.action = 'accrue'
      AND ual.source_order_id IS NOT NULL

    UNION ALL

    SELECT reversal.source_ledger_id AS ledger_id,
           po.id AS order_id,
           po.out_trade_no,
           reversal.inviter_user_id AS inviter_id,
           COALESCE(inviter.email, '') AS inviter_email,
           COALESCE(inviter.username, '') AS inviter_username,
           reversal.invitee_user_id AS invitee_id,
           COALESCE(invitee.email, '') AS invitee_email,
           COALESCE(invitee.username, '') AS invitee_username,
           po.amount::double precision AS order_amount,
           po.pay_amount::double precision AS pay_amount,
           reversal.rebate_amount::double precision AS rebate_amount,
           po.payment_type,
           po.status AS order_status,
           COALESCE(applied.created_at, reversal.created_at) AS rebate_created_at,
           'reversed' AS rebate_status,
           reversal.created_at AS reversed_at,
           reversal.reason AS reversal_reason,
           reversal.operator_user_id AS reversed_by_user_id,
           reversal.snapshot_available,
           reversal.frozen_quota_deducted::double precision AS frozen_quota_deducted,
           reversal.available_quota_deducted::double precision AS available_quota_deducted,
           reversal.balance_deducted::double precision AS balance_deducted,
           reversal.balance_before::double precision AS balance_before,
           reversal.balance_after::double precision AS balance_after
    FROM user_affiliate_reversals reversal
    JOIN payment_orders po ON po.id = reversal.source_order_id
    JOIN users invitee ON invitee.id = reversal.invitee_user_id
    JOIN users inviter ON inviter.id = reversal.inviter_user_id
    LEFT JOIN payment_audit_logs applied
      ON applied.order_id = po.id::text
     AND applied.action = 'AFFILIATE_REBATE_APPLIED'
    WHERE NOT EXISTS (
        SELECT 1
        FROM user_affiliate_ledger original
        WHERE original.action = 'accrue'
          AND original.source_order_id = reversal.source_order_id
    )
)
`

const rebateRecordsFilterSQL = `
WHERE ($1::timestamptz IS NULL OR rebate_created_at >= $1)
  AND ($2::timestamptz IS NULL OR rebate_created_at <= $2)
  AND (
      $3::text = ''
      OR LOWER(inviter_email) LIKE '%' || LOWER($3) || '%'
      OR LOWER(inviter_username) LIKE '%' || LOWER($3) || '%'
      OR LOWER(invitee_email) LIKE '%' || LOWER($3) || '%'
      OR LOWER(invitee_username) LIKE '%' || LOWER($3) || '%'
      OR LOWER(order_id::text) LIKE '%' || LOWER($3) || '%'
      OR LOWER(out_trade_no) LIKE '%' || LOWER($3) || '%'
      OR LOWER(payment_type) LIKE '%' || LOWER($3) || '%'
      OR LOWER(order_status) LIKE '%' || LOWER($3) || '%'
  )
  AND ($4::text = 'all' OR rebate_status = $4)
`

const rebateRecordsListSQL = rebateRecordsCTE + `
SELECT COUNT(*) OVER() AS record_total,
       ledger_id,
       order_id,
       out_trade_no,
       inviter_id,
       inviter_email,
       inviter_username,
       invitee_id,
       invitee_email,
       invitee_username,
       order_amount,
       pay_amount,
       rebate_amount,
       payment_type,
       order_status,
       rebate_created_at,
       rebate_status,
       reversed_at,
       reversal_reason,
       reversed_by_user_id,
       snapshot_available,
       frozen_quota_deducted,
       available_quota_deducted,
       balance_deducted,
       balance_before,
       balance_after
FROM rebate_records
` + rebateRecordsFilterSQL + `
ORDER BY
    CASE WHEN NOT $6 AND $5 = 'order' THEN order_id END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'order' THEN order_id END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'inviter' THEN inviter_email END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'inviter' THEN inviter_email END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'invitee' THEN invitee_email END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'invitee' THEN invitee_email END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'order_amount' THEN order_amount END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'order_amount' THEN order_amount END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'pay_amount' THEN pay_amount END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'pay_amount' THEN pay_amount END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'rebate_amount' THEN rebate_amount END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'rebate_amount' THEN rebate_amount END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'payment_type' THEN payment_type END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'payment_type' THEN payment_type END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'order_status' THEN order_status END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'order_status' THEN order_status END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'rebate_status' THEN rebate_status END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'rebate_status' THEN rebate_status END DESC NULLS LAST,
    CASE WHEN NOT $6 AND $5 = 'created_at' THEN rebate_created_at END ASC NULLS LAST,
    CASE WHEN $6 AND $5 = 'created_at' THEN rebate_created_at END DESC NULLS LAST,
    order_id DESC
LIMIT $7 OFFSET $8
`

const rebateRecordsCountSQL = rebateRecordsCTE + `
SELECT COUNT(*)
FROM rebate_records
` + rebateRecordsFilterSQL

func (s *Service) ListRebateRecords(ctx context.Context, filter RecordFilter) ([]RebateRecord, int64, error) {
	if s == nil || s.client == nil {
		return nil, 0, fmt.Errorf("affiliate reversal service unavailable")
	}
	filter = normalizeRecordFilter(filter)
	startAt := nullableTimeArg(filter.StartAt)
	endAt := nullableTimeArg(filter.EndAt)
	rows, err := s.client.QueryContext(ctx, rebateRecordsListSQL,
		startAt,
		endAt,
		filter.Search,
		filter.Status,
		filter.SortBy,
		filter.SortDesc,
		filter.PageSize,
		(filter.Page-1)*filter.PageSize,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list affiliate rebate records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]RebateRecord, 0, filter.PageSize)
	var total int64
	for rows.Next() {
		var item RebateRecord
		var ledgerID sql.NullInt64
		var reversedAt sql.NullTime
		var reversalReason sql.NullString
		var reversedBy sql.NullInt64
		var frozen, available, balance, balanceBefore, balanceAfter sql.NullFloat64
		if err := rows.Scan(
			&total,
			&ledgerID,
			&item.OrderID,
			&item.OutTradeNo,
			&item.InviterID,
			&item.InviterEmail,
			&item.InviterUsername,
			&item.InviteeID,
			&item.InviteeEmail,
			&item.InviteeUsername,
			&item.OrderAmount,
			&item.PayAmount,
			&item.RebateAmount,
			&item.PaymentType,
			&item.OrderStatus,
			&item.CreatedAt,
			&item.RebateStatus,
			&reversedAt,
			&reversalReason,
			&reversedBy,
			&item.SnapshotAvailable,
			&frozen,
			&available,
			&balance,
			&balanceBefore,
			&balanceAfter,
		); err != nil {
			return nil, 0, err
		}
		item.LedgerID = nullInt64Ptr(ledgerID)
		item.ReversedAt = nullTimePtr(reversedAt)
		item.ReversalReason = nullStringPtr(reversalReason)
		item.ReversedByUserID = nullInt64Ptr(reversedBy)
		item.FrozenQuotaDeducted = nullFloat64Ptr(frozen)
		item.AvailableQuotaDeducted = nullFloat64Ptr(available)
		item.BalanceDeducted = nullFloat64Ptr(balance)
		item.BalanceBefore = nullFloat64Ptr(balanceBefore)
		item.BalanceAfter = nullFloat64Ptr(balanceAfter)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		total, err = queryEmptyPageCount(ctx, s.client, startAt, endAt, filter.Search, filter.Status)
		if err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func normalizeRecordFilter(filter RecordFilter) RecordFilter {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > MaxBatchSize {
		filter.PageSize = MaxBatchSize
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Status = NormalizeStatus(filter.Status)
	filter.SortBy = normalizeSortBy(filter.SortBy)
	return filter
}

func normalizeSortBy(value string) string {
	switch strings.TrimSpace(value) {
	case "order", "inviter", "invitee", "order_amount", "pay_amount", "rebate_amount", "payment_type", "order_status", "rebate_status":
		return strings.TrimSpace(value)
	default:
		return "created_at"
	}
}

func queryEmptyPageCount(ctx context.Context, client queryExecer, startAt, endAt any, search, status string) (int64, error) {
	rows, err := client.QueryContext(ctx, rebateRecordsCountSQL, startAt, endAt, search, status)
	if err != nil {
		return 0, fmt.Errorf("count affiliate rebate records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var total int64
	if !rows.Next() {
		return 0, rows.Err()
	}
	if err := rows.Scan(&total); err != nil {
		return 0, err
	}
	return total, rows.Err()
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullFloat64Ptr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
