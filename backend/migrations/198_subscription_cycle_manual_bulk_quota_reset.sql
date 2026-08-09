-- 198: Allow manually administered subscription cycles to opt into bulk quota resets.

ALTER TABLE user_subscription_cycles
    ADD COLUMN IF NOT EXISTS manual_bulk_quota_reset_enabled BOOLEAN NOT NULL DEFAULT FALSE;
