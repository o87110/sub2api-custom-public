import {
  bulkResetQuota as requestBulkResetQuota,
  type BulkQuotaResetResult
} from '@/api/admin/subscriptions'

export const BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS = 300

export async function bulkResetQuota(
  subscriptionIds: number[],
  idempotencyKey: string
): Promise<BulkQuotaResetResult> {
  if (subscriptionIds.length > BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS) {
    throw new RangeError(
      `A bulk quota reset can contain at most ${BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS} subscriptions`
    )
  }

  return requestBulkResetQuota(subscriptionIds, idempotencyKey)
}
