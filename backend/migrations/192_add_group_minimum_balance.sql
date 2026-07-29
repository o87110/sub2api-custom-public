-- Migration: 192_add_group_minimum_balance
-- 分组级最低余额使用门槛。0 表示关闭；用户余额必须严格高于门槛才能绑定或使用分组。

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS minimum_balance DECIMAL(20,8) NOT NULL DEFAULT 0;

COMMENT ON COLUMN groups.minimum_balance
    IS 'Minimum user balance gate; 0 disables it and eligibility requires balance > minimum_balance';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE constraint_name = 'groups_minimum_balance_nonnegative'
          AND table_name = 'groups'
          AND table_schema = current_schema()
    ) THEN
        ALTER TABLE groups
            ADD CONSTRAINT groups_minimum_balance_nonnegative
            CHECK (minimum_balance >= 0);
    END IF;
END $$;
