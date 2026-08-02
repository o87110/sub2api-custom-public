import { adminAPI } from '@/api/admin'
import type {
  ContentModerationConfig,
  UpdateContentModerationConfig,
} from '@/api/admin/riskControl'

export interface ContentModerationAPIAuditScope {
  all_in_scope: boolean
  group_ids: number[]
}

export interface UserBanThresholdOverride {
  user_id: number
  ban_threshold: number
}

export interface CustomContentModerationConfig extends ContentModerationConfig {
  api_audit_scope?: ContentModerationAPIAuditScope
  user_ban_thresholds: UserBanThresholdOverride[]
}

export interface CustomUpdateContentModerationConfig extends UpdateContentModerationConfig {
  api_audit_scope?: ContentModerationAPIAuditScope
  user_ban_thresholds?: UserBanThresholdOverride[]
}

export const customRiskControlAPI = {
  async getConfig(): Promise<CustomContentModerationConfig> {
    return adminAPI.riskControl.getConfig() as Promise<CustomContentModerationConfig>
  },

  async updateConfig(
    payload: CustomUpdateContentModerationConfig
  ): Promise<CustomContentModerationConfig> {
    return adminAPI.riskControl.updateConfig(payload) as Promise<CustomContentModerationConfig>
  },
}
