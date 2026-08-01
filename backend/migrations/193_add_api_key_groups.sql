-- Migration: 193_add_api_key_groups
-- API Key 有序分组列表。api_keys.group_id 继续作为第一优先级兼容镜像。

CREATE TABLE IF NOT EXISTS api_key_groups (
    api_key_id BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    priority INTEGER NOT NULL,
    CONSTRAINT api_key_groups_pkey PRIMARY KEY (api_key_id, group_id),
    CONSTRAINT api_key_groups_api_key_priority_key UNIQUE (api_key_id, priority),
    CONSTRAINT api_key_groups_priority_nonnegative CHECK (priority >= 0),
    CONSTRAINT api_key_groups_api_key_id_fkey
        FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE,
    CONSTRAINT api_key_groups_group_id_fkey
        FOREIGN KEY (group_id) REFERENCES groups(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_api_key_groups_group_id
    ON api_key_groups(group_id);

-- Batch image jobs settle asynchronously, so persist the actual routed group
-- instead of consulting the API key's current primary group at settlement time.
ALTER TABLE batch_image_jobs
    ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_batch_image_jobs_group_id
    ON batch_image_jobs(group_id);

INSERT INTO api_key_groups (api_key_id, group_id, priority)
SELECT id, group_id, 0
FROM api_keys
WHERE group_id IS NOT NULL
ON CONFLICT (api_key_id, group_id) DO NOTHING;

COMMENT ON TABLE api_key_groups
    IS 'Ordered group assignments for API keys; lower priority values are preferred';
COMMENT ON COLUMN api_key_groups.priority
    IS 'Zero-based API key group priority; lower values are preferred';

COMMENT ON COLUMN batch_image_jobs.group_id
    IS 'Actual routed group captured when the asynchronous batch image job was submitted';

-- Migration 184/186 originally invalidated only api_keys.group_id. Ordered
-- assignments require every candidate position to participate in durable,
-- cross-instance auth-cache invalidation.
CREATE OR REPLACE FUNCTION enqueue_api_key_for_user_group_auth_cache_invalidation(
    target_user_id BIGINT,
    target_group_id BIGINT
)
RETURNS VOID
LANGUAGE plpgsql
AS $$
BEGIN
    IF target_user_id IS NULL OR target_group_id IS NULL THEN
        RETURN;
    END IF;
    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT DISTINCT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.user_id = target_user_id
      AND k.deleted_at IS NULL
      AND k.key <> ''
      AND (
          k.group_id = target_group_id
          OR EXISTS (
              SELECT 1
              FROM api_key_groups AS akg
              WHERE akg.api_key_id = k.id
                AND akg.group_id = target_group_id
          )
      );
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_group_id BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target_group_id := OLD.id;
    ELSE
        target_group_id := NEW.id;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT DISTINCT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.deleted_at IS NULL
      AND k.key <> ''
      AND (
          k.group_id = target_group_id
          OR EXISTS (
              SELECT 1
              FROM api_key_groups AS akg
              WHERE akg.api_key_id = k.id
                AND akg.group_id = target_group_id
          )
      );

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_allowed_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM enqueue_api_key_for_user_group_auth_cache_invalidation(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP IN ('UPDATE', 'INSERT') THEN
        PERFORM enqueue_api_key_for_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION enqueue_api_key_group_assignment_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    old_api_key_id BIGINT;
    new_api_key_id BIGINT;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        old_api_key_id := OLD.api_key_id;
        INSERT INTO auth_cache_invalidation_outbox (cache_key)
        SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
        FROM api_keys AS k
        WHERE k.id = old_api_key_id
          AND k.deleted_at IS NULL
          AND k.key <> '';
    END IF;
    IF TG_OP IN ('UPDATE', 'INSERT') THEN
        new_api_key_id := NEW.api_key_id;
        IF old_api_key_id IS DISTINCT FROM new_api_key_id THEN
            INSERT INTO auth_cache_invalidation_outbox (cache_key)
            SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
            FROM api_keys AS k
            WHERE k.id = new_api_key_id
              AND k.deleted_at IS NULL
              AND k.key <> '';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_api_key_groups_auth_cache_invalidation ON api_key_groups;
CREATE TRIGGER trg_api_key_groups_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON api_key_groups
FOR EACH ROW EXECUTE FUNCTION enqueue_api_key_group_assignment_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_user_group_rate_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM enqueue_api_key_for_user_group_auth_cache_invalidation(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP IN ('UPDATE', 'INSERT') THEN
        PERFORM enqueue_api_key_for_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_group_rates_auth_cache_invalidation ON user_group_rate_multipliers;
CREATE TRIGGER trg_user_group_rates_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON user_group_rate_multipliers
FOR EACH ROW EXECUTE FUNCTION enqueue_user_group_rate_auth_cache_invalidation();

CREATE OR REPLACE FUNCTION enqueue_user_subscription_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM enqueue_api_key_for_user_group_auth_cache_invalidation(OLD.user_id, OLD.group_id);
    END IF;
    IF TG_OP IN ('UPDATE', 'INSERT') THEN
        PERFORM enqueue_api_key_for_user_group_auth_cache_invalidation(NEW.user_id, NEW.group_id);
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_user_subscriptions_auth_cache_invalidation ON user_subscriptions;
CREATE TRIGGER trg_user_subscriptions_auth_cache_invalidation
AFTER INSERT OR UPDATE OR DELETE ON user_subscriptions
FOR EACH ROW EXECUTE FUNCTION enqueue_user_subscription_auth_cache_invalidation();
