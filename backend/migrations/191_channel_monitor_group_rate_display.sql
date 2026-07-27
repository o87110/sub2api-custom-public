-- Migration: 191_channel_monitor_group_rate_display
-- 渠道监控增加仅用于用户卡片展示的倍率覆盖值和显示模板。
-- 不修改分组真实倍率、计费、调度或 API Key；历史数据保持自动解析行为。

ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS group_rate_override DECIMAL(10,4) NULL,
    ADD COLUMN IF NOT EXISTS group_rate_display_template VARCHAR(64) NOT NULL DEFAULT '';

COMMENT ON COLUMN channel_monitors.group_rate_override
    IS 'Optional display-only group rate override; must be positive';
COMMENT ON COLUMN channel_monitors.group_rate_display_template
    IS 'Display template containing exactly one {rate}; empty means {rate}x';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM information_schema.table_constraints
        WHERE constraint_name = 'channel_monitors_group_rate_override_check'
          AND table_name = 'channel_monitors'
          AND table_schema = current_schema()
    ) THEN
        ALTER TABLE channel_monitors
            ADD CONSTRAINT channel_monitors_group_rate_override_check
            CHECK (group_rate_override IS NULL OR group_rate_override > 0);
    END IF;
END $$;
