import { adminAPI } from '@/api/admin'
import type {
  ContentModerationConfig,
  UpdateContentModerationConfig,
} from '@/api/admin/riskControl'

export interface ContentModerationAPIAuditScope {
  all_in_scope: boolean
  group_ids: number[]
}

export interface CustomContentModerationConfig extends ContentModerationConfig {
  api_audit_scope?: ContentModerationAPIAuditScope
}

export interface CustomUpdateContentModerationConfig extends UpdateContentModerationConfig {
  api_audit_scope?: ContentModerationAPIAuditScope
}

export const customRiskControlAPI = {
  async getConfig(): Promise<CustomContentModerationConfig> {
    return adminAPI.riskControl.getConfig()
  },

  async updateConfig(
    payload: CustomUpdateContentModerationConfig
  ): Promise<CustomContentModerationConfig> {
    return adminAPI.riskControl.updateConfig(payload)
  },
}
