-- 196: Track display-only subscription-cycle usage and manual quota resets.
-- Quota-window counters remain authoritative for enforcement.

ALTER TABLE user_subscriptions
    ADD COLUMN IF NOT EXISTS cycle_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS manual_quota_reset_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS current_cycle_starts_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS current_cycle_ends_at TIMESTAMPTZ;

UPDATE user_subscriptions
SET cycle_usage_usd = GREATEST(
        daily_usage_usd,
        weekly_usage_usd,
        monthly_usage_usd,
        COALESCE((
            SELECT SUM(ul.actual_cost)
            FROM usage_logs ul
            WHERE ul.subscription_id = user_subscriptions.id
              AND ul.created_at >= user_subscriptions.starts_at
              AND ul.created_at < user_subscriptions.expires_at
        ), 0)
    ),
    current_cycle_starts_at = COALESCE(current_cycle_starts_at, starts_at),
    current_cycle_ends_at = COALESCE(current_cycle_ends_at, expires_at)
WHERE current_cycle_starts_at IS NULL
   OR current_cycle_ends_at IS NULL;

ALTER TABLE user_subscriptions
	ALTER COLUMN current_cycle_starts_at SET NOT NULL,
	ALTER COLUMN current_cycle_ends_at SET NOT NULL,
	ALTER COLUMN current_cycle_starts_at SET DEFAULT NOW(),
	ALTER COLUMN current_cycle_ends_at SET DEFAULT NOW();

CREATE TABLE IF NOT EXISTS user_subscription_cycles (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT NOT NULL REFERENCES user_subscriptions(id) ON DELETE CASCADE,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    source_type VARCHAR(32) NOT NULL DEFAULT 'assignment',
    source_ref VARCHAR(255),
    final_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0,
    final_manual_quota_reset_count BIGINT NOT NULL DEFAULT 0,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_subscription_cycles_subscription_start
    ON user_subscription_cycles(subscription_id, starts_at);
CREATE INDEX IF NOT EXISTS idx_user_subscription_cycles_subscription_status
    ON user_subscription_cycles(subscription_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_subscription_cycles_source
    ON user_subscription_cycles(source_type, source_ref)
    WHERE source_ref IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_subscription_cycles_current
    ON user_subscription_cycles(subscription_id)
    WHERE status = 'current';

INSERT INTO user_subscription_cycles (
    subscription_id,
    starts_at,
    ends_at,
    status,
    source_type,
    final_usage_usd,
    final_manual_quota_reset_count,
    completed_at
)
SELECT
    id,
    current_cycle_starts_at,
    current_cycle_ends_at,
    CASE
        WHEN status = 'active' AND expires_at > NOW() THEN 'current'
        ELSE 'completed'
    END,
    'legacy',
    CASE WHEN status = 'active' AND expires_at > NOW() THEN 0 ELSE cycle_usage_usd END,
    CASE WHEN status = 'active' AND expires_at > NOW() THEN 0 ELSE manual_quota_reset_count END,
    CASE WHEN status = 'active' AND expires_at > NOW() THEN NULL ELSE NOW() END
FROM user_subscriptions us
WHERE NOT EXISTS (
    SELECT 1
    FROM user_subscription_cycles usc
    WHERE usc.subscription_id = us.id
);
