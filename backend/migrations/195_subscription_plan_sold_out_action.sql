-- Configure how limited subscription plans behave after inventory reaches zero.
-- Existing plans keep the historical automatic-delisting behavior.
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS sold_out_action VARCHAR(32) NOT NULL DEFAULT 'delist';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'subscription_plans_sold_out_action_valid'
          AND conrelid = 'subscription_plans'::regclass
    ) THEN
        ALTER TABLE subscription_plans
            ADD CONSTRAINT subscription_plans_sold_out_action_valid
            CHECK (sold_out_action IN ('delist', 'disable_purchase'));
    END IF;
END $$;
