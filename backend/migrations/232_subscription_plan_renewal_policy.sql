-- Allow eligible existing subscribers to renew plans that are no longer
-- available to new buyers. Defaults preserve the historical behavior.
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS allow_existing_user_renewal BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS renewal_grace_days INTEGER NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'subscription_plans_renewal_grace_days_valid'
          AND conrelid = 'subscription_plans'::regclass
    ) THEN
        ALTER TABLE subscription_plans
            ADD CONSTRAINT subscription_plans_renewal_grace_days_valid
            CHECK (renewal_grace_days >= 0 AND renewal_grace_days <= 30);
    END IF;
END $$;
