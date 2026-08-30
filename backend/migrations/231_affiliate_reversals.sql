-- 邀请返利撤销审计表。
-- 原返利流水保留不删除，仅在撤销时把业务状态标记为 reversed；撤销记录保存资金扣减去向和操作前后快照。

CREATE TABLE IF NOT EXISTS user_affiliate_reversals (
    id BIGSERIAL PRIMARY KEY,
    source_ledger_id BIGINT NULL REFERENCES user_affiliate_ledger(id) ON DELETE RESTRICT,
    source_order_id BIGINT NOT NULL REFERENCES payment_orders(id) ON DELETE RESTRICT,
    inviter_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    invitee_user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    rebate_amount DECIMAL(20,8) NOT NULL,
    frozen_quota_deducted DECIMAL(20,8) NULL,
    available_quota_deducted DECIMAL(20,8) NULL,
    balance_deducted DECIMAL(20,8) NULL,
    total_recharged_deducted DECIMAL(20,8) NULL,
    balance_before DECIMAL(20,8) NULL,
    balance_after DECIMAL(20,8) NULL,
    aff_quota_before DECIMAL(20,8) NULL,
    aff_quota_after DECIMAL(20,8) NULL,
    aff_frozen_quota_before DECIMAL(20,8) NULL,
    aff_frozen_quota_after DECIMAL(20,8) NULL,
    aff_history_quota_before DECIMAL(20,8) NULL,
    aff_history_quota_after DECIMAL(20,8) NULL,
    total_recharged_before DECIMAL(20,8) NULL,
    total_recharged_after DECIMAL(20,8) NULL,
    snapshot_available BOOLEAN NOT NULL DEFAULT TRUE,
    reason TEXT NOT NULL,
    operator_user_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    operation_key_hash VARCHAR(64) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_affiliate_reversals_source_order_unique UNIQUE (source_order_id),
    CONSTRAINT user_affiliate_reversals_source_ledger_unique UNIQUE (source_ledger_id),
    CONSTRAINT user_affiliate_reversals_positive_amount CHECK (rebate_amount > 0),
    CONSTRAINT user_affiliate_reversals_nonnegative_deductions CHECK (
        (frozen_quota_deducted IS NULL OR frozen_quota_deducted >= 0)
        AND (available_quota_deducted IS NULL OR available_quota_deducted >= 0)
        AND (balance_deducted IS NULL OR balance_deducted >= 0)
        AND (total_recharged_deducted IS NULL OR total_recharged_deducted >= 0)
    ),
    CONSTRAINT user_affiliate_reversals_snapshot_shape CHECK (
        snapshot_available = FALSE
        OR (
            frozen_quota_deducted IS NOT NULL
            AND available_quota_deducted IS NOT NULL
            AND balance_deducted IS NOT NULL
            AND total_recharged_deducted IS NOT NULL
            AND balance_before IS NOT NULL
            AND balance_after IS NOT NULL
            AND aff_quota_before IS NOT NULL
            AND aff_quota_after IS NOT NULL
            AND aff_frozen_quota_before IS NOT NULL
            AND aff_frozen_quota_after IS NOT NULL
            AND aff_history_quota_before IS NOT NULL
            AND aff_history_quota_after IS NOT NULL
            AND total_recharged_before IS NOT NULL
            AND total_recharged_after IS NOT NULL
            AND rebate_amount = frozen_quota_deducted + available_quota_deducted + balance_deducted
            AND total_recharged_deducted = balance_deducted
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_user_affiliate_reversals_inviter_created
    ON user_affiliate_reversals(inviter_user_id, created_at);

CREATE INDEX IF NOT EXISTS idx_user_affiliate_reversals_invitee_created
    ON user_affiliate_reversals(invitee_user_id, created_at);

CREATE INDEX IF NOT EXISTS idx_user_affiliate_reversals_operation_key
    ON user_affiliate_reversals(operation_key_hash);

COMMENT ON TABLE user_affiliate_reversals IS '邀请返利撤销审计；原返利、订单和支付审计均保留';
COMMENT ON COLUMN user_affiliate_reversals.source_ledger_id IS '原返利流水；历史手工删除的流水无法关联时为 NULL';
COMMENT ON COLUMN user_affiliate_reversals.snapshot_available IS '是否具备完整资金扣减与前后快照';
COMMENT ON COLUMN user_affiliate_reversals.operation_key_hash IS '管理端幂等操作哈希，不保存原始 Idempotency-Key';

-- 通用回填历史手工撤销：payment_audit_logs 已保留订单、原流水 ID、金额和撤销时间。
-- 历史记录没有可靠资金前后快照，因此明确标记 snapshot_available=false，避免伪造数据。
WITH legacy_audits AS (
    SELECT pal.id AS audit_id,
           po.id AS order_id,
           po.user_id AS invitee_user_id,
           invitee_aff.inviter_id AS inviter_user_id,
           substring(
               pal.detail
               FROM '"rebateAmount"[[:space:]]*:[[:space:]]*(-?[0-9]+(\.[0-9]+)?)'
           )::numeric AS rebate_amount,
           substring(
               pal.detail
               FROM '"sourceLedgerId"[[:space:]]*:[[:space:]]*([0-9]+)'
           )::bigint AS recorded_ledger_id,
           pal.created_at
    FROM payment_audit_logs pal
    JOIN payment_orders po ON po.id::text = pal.order_id
    JOIN user_affiliates invitee_aff ON invitee_aff.user_id = po.user_id
    WHERE pal.action = 'AFFILIATE_REBATE_REVERSED'
),
resolved AS (
    SELECT la.*,
           ual.id AS source_ledger_id
    FROM legacy_audits la
    LEFT JOIN user_affiliate_ledger ual
      ON ual.id = la.recorded_ledger_id
     AND ual.source_order_id = la.order_id
     AND ual.action = 'accrue'
    WHERE la.inviter_user_id IS NOT NULL
      AND la.rebate_amount > 0
)
INSERT INTO user_affiliate_reversals (
    source_ledger_id,
    source_order_id,
    inviter_user_id,
    invitee_user_id,
    rebate_amount,
    snapshot_available,
    reason,
    operator_user_id,
    operation_key_hash,
    created_at,
    updated_at
)
SELECT source_ledger_id,
       order_id,
       inviter_user_id,
       invitee_user_id,
       rebate_amount,
       FALSE,
       'legacy manual affiliate reversal',
       NULL,
       NULL,
       created_at,
       created_at
FROM resolved
ON CONFLICT (source_order_id) DO NOTHING;

-- 历史记录若仍保留原流水，则只把业务状态标记为 reversed；金额、来源、订单和时间不改写。
-- 清空 frozen_until 可防止旧版本懒解冻逻辑把已撤销金额重新移入可用额度。
UPDATE user_affiliate_ledger ledger
SET action = 'reversed',
    frozen_until = NULL,
    updated_at = NOW()
FROM user_affiliate_reversals reversal
WHERE reversal.source_ledger_id = ledger.id
  AND ledger.action = 'accrue';

COMMENT ON COLUMN user_affiliate_ledger.action IS 'accrue|transfer|reversed';
