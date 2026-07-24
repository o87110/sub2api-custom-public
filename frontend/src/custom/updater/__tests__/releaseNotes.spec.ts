import { describe, expect, it } from 'vitest'

import { summarizeReleaseNotes } from '@/custom/updater/releaseNotes'

describe('summarizeReleaseNotes', () => {
  it('keeps the custom change summary and removes release boilerplate', () => {
    const body = `> AI API Gateway Platform - 将 AI 订阅配额分发和管理

Based on official v0.1.161.
Source commit: 1b539ccd0b94106a40d66f26ba2c3c7238946389

修复发布说明安全传递、公开仓库回退入口、基础版本显示及 GHCR 通知信息。

---

## 📥 Installation

**Docker:**
\`\`\`bash
docker pull ghcr.io/o87110/sub2api-custom-public:v0.1.161-custom.2
\`\`\`

## 📚 Documentation

- [GitHub Repository](https://github.com/o87110/sub2api-custom-public)`

    expect(summarizeReleaseNotes(body)).toBe(
      '修复发布说明安全传递、公开仓库回退入口、基础版本显示及 GHCR 通知信息。'
    )
  })

  it('turns normal Markdown release notes into compact plain text', () => {
    const body = `## What's Changed

- Fix **update comparison**
- See [details](https://example.com/release)`

    expect(summarizeReleaseNotes(body)).toBe(
      "What's Changed Fix update comparison See details"
    )
  })

  it('truncates long summaries and handles empty content', () => {
    expect(summarizeReleaseNotes('1234567890', 6)).toBe('12345…')
    expect(summarizeReleaseNotes('   ')).toBe('')
  })
})
