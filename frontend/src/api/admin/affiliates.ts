/**
 * Admin Affiliate API endpoints
 * Manage per-user affiliate (邀请返利) configurations:
 * exclusive invite codes (overrides aff_code) and exclusive rebate rates.
 */

import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface AffiliateAdminEntry {
  user_id: number
  email: string
  username: string
  aff_code: string
  aff_code_custom: boolean
  aff_rebate_rate_percent?: number | null
  aff_count: number
}

export interface ListAffiliateUsersParams {
  page?: number
  page_size?: number
  search?: string
}

export interface ListAffiliateRecordsParams {
  page?: number
  page_size?: number
  search?: string
  start_at?: string
  end_at?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
  timezone?: string
  rebate_status?: 'all' | 'active' | 'reversed'
}

export interface AffiliateInviteRecord {
  inviter_id: number
  inviter_email: string
  inviter_username: string
  invitee_id: number
  invitee_email: string
  invitee_username: string
  aff_code: string
  total_rebate: number
  created_at: string
}

export interface AffiliateRebateRecord {
  ledger_id?: number | null
  order_id: number
  out_trade_no: string
  inviter_id: number
  inviter_email: string
  inviter_username: string
  invitee_id: number
  invitee_email: string
  invitee_username: string
  order_amount: number
  pay_amount: number
  rebate_amount: number
  payment_type: string
  order_status: string
  rebate_status: 'active' | 'reversed'
  reversed_at?: string | null
  reversal_reason?: string | null
  reversed_by_user_id?: number | null
  snapshot_available: boolean
  frozen_quota_deducted?: number | null
  available_quota_deducted?: number | null
  balance_deducted?: number | null
  balance_before?: number | null
  balance_after?: number | null
  created_at: string
}

export interface AffiliateReversalPreviewOrder {
  order_id: number
  ledger_id: number
  inviter_id: number
  invitee_id: number
  rebate_amount: number
  quota_bucket: 'frozen' | 'available'
  frozen_quota_deducted: number
  available_quota_deducted: number
  balance_deducted: number
}

export interface AffiliateReversalInviterImpact {
  inviter_id: number
  inviter_email: string
  inviter_username: string
  order_count: number
  total_rebate_amount: number
  frozen_quota_deducted: number
  available_quota_deducted: number
  balance_deducted: number
  balance_before: number
  balance_after: number
  history_quota_before: number
  history_quota_after: number
  total_recharged_before: number
  total_recharged_after: number
  will_be_negative: boolean
}

export interface AffiliateReversalPreview {
  preview_token: string
  order_count: number
  total_rebate_amount: number
  total_balance_deducted: number
  negative_balance_users: number
  has_negative_balance: boolean
  orders: AffiliateReversalPreviewOrder[]
  inviters: AffiliateReversalInviterImpact[]
}

export interface AffiliateReversalResult {
  reversed_count: number
  total_rebate_amount: number
  total_balance_deducted: number
  negative_balance_users: number
  orders: Array<{
    reversal_id: number
    order_id: number
    ledger_id: number
    inviter_id: number
    invitee_id: number
    rebate_amount: number
    frozen_quota_deducted: number
    available_quota_deducted: number
    balance_deducted: number
  }>
  inviters: AffiliateReversalInviterImpact[]
}

export interface ReverseAffiliateRebatesRequest {
  order_ids: number[]
  preview_token: string
  reason: string
  confirm_negative_balance: boolean
}

export interface AffiliateTransferRecord {
  ledger_id: number
  user_id: number
  user_email: string
  username: string
  amount: number
  balance_after?: number | null
  available_quota_after?: number | null
  frozen_quota_after?: number | null
  history_quota_after?: number | null
  snapshot_available: boolean
  created_at: string
}

export interface AffiliateUserOverview {
  user_id: number
  email: string
  username: string
  aff_code: string
  rebate_rate_percent: number
  invited_count: number
  rebated_invitee_count: number
  available_quota: number
  history_quota: number
}

export interface UpdateAffiliateUserRequest {
  aff_code?: string
  aff_rebate_rate_percent?: number | null
  /** Set true to explicitly clear the per-user rate (sets it to NULL). */
  clear_rebate_rate?: boolean
}

export interface BatchSetRateRequest {
  user_ids: number[]
  aff_rebate_rate_percent?: number | null
  /** Set true to clear rates instead of setting. */
  clear?: boolean
}

export interface SimpleUser {
  id: number
  email: string
  username: string
}

export async function listUsers(
  params: ListAffiliateUsersParams = {},
): Promise<PaginatedResponse<AffiliateAdminEntry>> {
  const { data } = await apiClient.get<PaginatedResponse<AffiliateAdminEntry>>(
    '/admin/affiliates/users',
    {
      params: {
        page: params.page ?? 1,
        page_size: params.page_size ?? 20,
        search: params.search ?? '',
      },
    },
  )
  return data
}

export async function lookupUsers(q: string): Promise<SimpleUser[]> {
  const { data } = await apiClient.get<SimpleUser[]>(
    '/admin/affiliates/users/lookup',
    { params: { q } },
  )
  return data
}

export async function updateUserSettings(
  userId: number,
  payload: UpdateAffiliateUserRequest,
): Promise<{ user_id: number }> {
  const { data } = await apiClient.put<{ user_id: number }>(
    `/admin/affiliates/users/${userId}`,
    payload,
  )
  return data
}

export async function clearUserSettings(
  userId: number,
): Promise<{ user_id: number }> {
  const { data } = await apiClient.delete<{ user_id: number }>(
    `/admin/affiliates/users/${userId}`,
  )
  return data
}

export async function batchSetRate(
  payload: BatchSetRateRequest,
): Promise<{ affected: number }> {
  const { data } = await apiClient.post<{ affected: number }>(
    '/admin/affiliates/users/batch-rate',
    payload,
  )
  return data
}

function recordParams(params: ListAffiliateRecordsParams = {}) {
  return {
    page: params.page ?? 1,
    page_size: params.page_size ?? 20,
    search: params.search ?? '',
    start_at: params.start_at || undefined,
    end_at: params.end_at || undefined,
    sort_by: params.sort_by || undefined,
    sort_order: params.sort_order || undefined,
    timezone: params.timezone || undefined,
    rebate_status: params.rebate_status || undefined,
  }
}

export async function listInviteRecords(
  params: ListAffiliateRecordsParams = {},
): Promise<PaginatedResponse<AffiliateInviteRecord>> {
  const { data } = await apiClient.get<PaginatedResponse<AffiliateInviteRecord>>(
    '/admin/affiliates/invites',
    { params: recordParams(params) },
  )
  return data
}

export async function listRebateRecords(
  params: ListAffiliateRecordsParams = {},
): Promise<PaginatedResponse<AffiliateRebateRecord>> {
  const { data } = await apiClient.get<PaginatedResponse<AffiliateRebateRecord>>(
    '/admin/affiliates/rebates',
    { params: recordParams(params) },
  )
  return data
}

export async function listTransferRecords(
  params: ListAffiliateRecordsParams = {},
): Promise<PaginatedResponse<AffiliateTransferRecord>> {
  const { data } = await apiClient.get<PaginatedResponse<AffiliateTransferRecord>>(
    '/admin/affiliates/transfers',
    { params: recordParams(params) },
  )
  return data
}

export async function getUserOverview(
  userId: number,
): Promise<AffiliateUserOverview> {
  const { data } = await apiClient.get<AffiliateUserOverview>(
    `/admin/affiliates/users/${userId}/overview`,
  )
  return data
}

export async function previewRebateReversal(
  orderIds: number[],
): Promise<AffiliateReversalPreview> {
  const { data } = await apiClient.post<AffiliateReversalPreview>(
    '/admin/affiliates/rebates/reversal-preview',
    { order_ids: orderIds },
  )
  return data
}

export async function reverseRebates(
  payload: ReverseAffiliateRebatesRequest,
  idempotencyKey: string,
): Promise<AffiliateReversalResult> {
  const { data } = await apiClient.post<AffiliateReversalResult>(
    '/admin/affiliates/rebates/reverse',
    payload,
    { headers: { 'Idempotency-Key': idempotencyKey } },
  )
  return data
}

export const affiliatesAPI = {
  listUsers,
  lookupUsers,
  updateUserSettings,
  clearUserSettings,
  batchSetRate,
  listInviteRecords,
  listRebateRecords,
  listTransferRecords,
  getUserOverview,
  previewRebateReversal,
  reverseRebates,
}

export default affiliatesAPI
