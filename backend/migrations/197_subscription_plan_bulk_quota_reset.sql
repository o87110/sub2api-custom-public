-- 197: Allow administrators to opt subscription plans into bulk quota resets.

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS allow_bulk_quota_reset BOOLEAN NOT NULL DEFAULT FALSE;
