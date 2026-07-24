export type CustomUpdaterTextKey =
  | 'customRelease'
  | 'officialRelease'
  | 'viewCustomRelease'
  | 'viewOfficialRelease'
  | 'officialUpdateAvailable'
  | 'officialReleaseUnavailable'
  | 'versionCheckFailed'

const messages: Record<'zh' | 'en', Record<CustomUpdaterTextKey, string>> = {
  zh: {
    customRelease: '二改发布',
    officialRelease: '官方发布',
    viewCustomRelease: '查看二改发布',
    viewOfficialRelease: '查看官方发布',
    officialUpdateAvailable: '官方有新版本',
    officialReleaseUnavailable: '官方发布不可用',
    versionCheckFailed: '版本检查失败'
  },
  en: {
    customRelease: 'Custom Release',
    officialRelease: 'Official Release',
    viewCustomRelease: 'View Custom Release',
    viewOfficialRelease: 'View Official Release',
    officialUpdateAvailable: 'Official newer',
    officialReleaseUnavailable: 'Official release unavailable',
    versionCheckFailed: 'Version check failed'
  }
}

export function customUpdaterText(locale: string, key: CustomUpdaterTextKey): string {
  return messages[locale.toLowerCase().startsWith('zh') ? 'zh' : 'en'][key]
}
