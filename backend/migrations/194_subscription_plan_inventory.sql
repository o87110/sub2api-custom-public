-- Optional inventory for subscription plans. NULL keeps the historical
-- unlimited-sale behavior; zero is produced only by runtime reservations.
ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS remaining_quantity INTEGER;

ALTER TABLE subscription_plans
    ADD COLUMN IF NOT EXISTS inventory_auto_delisted BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS plan_inventory_state VARCHAR(16) NOT NULL DEFAULT 'untracked';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'subscription_plans_remaining_quantity_nonnegative'
          AND conrelid = 'subscription_plans'::regclass
    ) THEN
        ALTER TABLE subscription_plans
            ADD CONSTRAINT subscription_plans_remaining_quantity_nonnegative
            CHECK (remaining_quantity IS NULL OR remaining_quantity >= 0);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'payment_orders_plan_inventory_state_valid'
          AND conrelid = 'payment_orders'::regclass
    ) THEN
        ALTER TABLE payment_orders
            ADD CONSTRAINT payment_orders_plan_inventory_state_valid
            CHECK (plan_inventory_state IN ('untracked', 'reserved', 'consumed', 'released'));
    END IF;
END $$;
