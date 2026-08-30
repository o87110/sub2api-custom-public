package affiliatereversal

import (
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	MaxBatchSize = 100
	MaxReasonLen = 500

	StatusAll      = "all"
	StatusActive   = "active"
	StatusReversed = "reversed"
)

var (
	ErrInvalidOrders        = infraerrors.BadRequest("AFFILIATE_REVERSAL_INVALID_ORDERS", "affiliate rebate order_ids must contain 1 to 100 unique positive IDs")
	ErrInvalidReason        = infraerrors.BadRequest("AFFILIATE_REVERSAL_INVALID_REASON", "affiliate reversal reason is required and must not exceed 500 characters")
	ErrTargetInvalid        = infraerrors.Conflict("AFFILIATE_REVERSAL_TARGET_INVALID", "one or more affiliate rebates are missing, duplicated, or already reversed")
	ErrPreviewStale         = infraerrors.Conflict("AFFILIATE_REVERSAL_PREVIEW_STALE", "affiliate reversal preview is stale; preview the selected orders again")
	ErrNegativeConfirmation = infraerrors.Conflict("AFFILIATE_REVERSAL_NEGATIVE_BALANCE_CONFIRM_REQUIRED", "confirm_negative_balance is required because the reversal will create a negative balance")
	ErrInvariant            = infraerrors.Conflict("AFFILIATE_REVERSAL_INVARIANT", "affiliate reversal cannot continue because the stored financial totals are inconsistent")
)

type RecordFilter struct {
	Search   string
	Page     int
	PageSize int
	StartAt  *time.Time
	EndAt    *time.Time
	SortBy   string
	SortDesc bool
	Status   string
}

type RebateRecord struct {
	LedgerID               *int64     `json:"ledger_id,omitempty"`
	OrderID                int64      `json:"order_id"`
	OutTradeNo             string     `json:"out_trade_no"`
	InviterID              int64      `json:"inviter_id"`
	InviterEmail           string     `json:"inviter_email"`
	InviterUsername        string     `json:"inviter_username"`
	InviteeID              int64      `json:"invitee_id"`
	InviteeEmail           string     `json:"invitee_email"`
	InviteeUsername        string     `json:"invitee_username"`
	OrderAmount            float64    `json:"order_amount"`
	PayAmount              float64    `json:"pay_amount"`
	RebateAmount           float64    `json:"rebate_amount"`
	PaymentType            string     `json:"payment_type"`
	OrderStatus            string     `json:"order_status"`
	RebateStatus           string     `json:"rebate_status"`
	CreatedAt              time.Time  `json:"created_at"`
	ReversedAt             *time.Time `json:"reversed_at,omitempty"`
	ReversalReason         *string    `json:"reversal_reason,omitempty"`
	ReversedByUserID       *int64     `json:"reversed_by_user_id,omitempty"`
	SnapshotAvailable      bool       `json:"snapshot_available"`
	FrozenQuotaDeducted    *float64   `json:"frozen_quota_deducted,omitempty"`
	AvailableQuotaDeducted *float64   `json:"available_quota_deducted,omitempty"`
	BalanceDeducted        *float64   `json:"balance_deducted,omitempty"`
	BalanceBefore          *float64   `json:"balance_before,omitempty"`
	BalanceAfter           *float64   `json:"balance_after,omitempty"`
}

type PreviewOrder struct {
	OrderID         int64   `json:"order_id"`
	LedgerID        int64   `json:"ledger_id"`
	InviterID       int64   `json:"inviter_id"`
	InviteeID       int64   `json:"invitee_id"`
	RebateAmount    float64 `json:"rebate_amount"`
	QuotaBucket     string  `json:"quota_bucket"`
	FrozenDeducted  float64 `json:"frozen_quota_deducted"`
	QuotaDeducted   float64 `json:"available_quota_deducted"`
	BalanceDeducted float64 `json:"balance_deducted"`
}

type InviterImpact struct {
	InviterID              int64   `json:"inviter_id"`
	InviterEmail           string  `json:"inviter_email"`
	InviterUsername        string  `json:"inviter_username"`
	OrderCount             int     `json:"order_count"`
	TotalRebateAmount      float64 `json:"total_rebate_amount"`
	FrozenQuotaDeducted    float64 `json:"frozen_quota_deducted"`
	AvailableQuotaDeducted float64 `json:"available_quota_deducted"`
	BalanceDeducted        float64 `json:"balance_deducted"`
	BalanceBefore          float64 `json:"balance_before"`
	BalanceAfter           float64 `json:"balance_after"`
	HistoryQuotaBefore     float64 `json:"history_quota_before"`
	HistoryQuotaAfter      float64 `json:"history_quota_after"`
	TotalRechargedBefore   float64 `json:"total_recharged_before"`
	TotalRechargedAfter    float64 `json:"total_recharged_after"`
	WillBeNegative         bool    `json:"will_be_negative"`
}

type Preview struct {
	PreviewToken         string          `json:"preview_token"`
	OrderCount           int             `json:"order_count"`
	TotalRebateAmount    float64         `json:"total_rebate_amount"`
	TotalBalanceDeducted float64         `json:"total_balance_deducted"`
	NegativeBalanceUsers int             `json:"negative_balance_users"`
	HasNegativeBalance   bool            `json:"has_negative_balance"`
	Orders               []PreviewOrder  `json:"orders"`
	Inviters             []InviterImpact `json:"inviters"`
}

type ReverseInput struct {
	OrderIDs               []int64 `json:"order_ids"`
	PreviewToken           string  `json:"preview_token"`
	Reason                 string  `json:"reason"`
	ConfirmNegativeBalance bool    `json:"confirm_negative_balance"`
}

type ReverseOrderResult struct {
	ReversalID             int64   `json:"reversal_id"`
	OrderID                int64   `json:"order_id"`
	LedgerID               int64   `json:"ledger_id"`
	InviterID              int64   `json:"inviter_id"`
	InviteeID              int64   `json:"invitee_id"`
	RebateAmount           float64 `json:"rebate_amount"`
	FrozenQuotaDeducted    float64 `json:"frozen_quota_deducted"`
	AvailableQuotaDeducted float64 `json:"available_quota_deducted"`
	BalanceDeducted        float64 `json:"balance_deducted"`
}

type ReverseResult struct {
	ReversedCount        int                  `json:"reversed_count"`
	TotalRebateAmount    float64              `json:"total_rebate_amount"`
	TotalBalanceDeducted float64              `json:"total_balance_deducted"`
	NegativeBalanceUsers int                  `json:"negative_balance_users"`
	Orders               []ReverseOrderResult `json:"orders"`
	Inviters             []InviterImpact      `json:"inviters"`
}
