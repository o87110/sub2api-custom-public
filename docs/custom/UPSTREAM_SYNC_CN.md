# 同步官方版本与升级门禁

## 1. 目标

公开仓库只同步 `Wei-Shaw/sub2api` 的正式 `vX.Y.Z` Tag，不直接跟随未发布的
`upstream/main`。升级必须保持二改功能、数据库安全、可复现分支和完整审查证据。

## 2. 引用与分支

| 引用 | 含义 |
| --- | --- |
| `main` | 当前公开二改主线 |
| `upstream/main` | 最近完成审查的官方镜像提交 |
| `refs/upstream-tags/v*` | 从官方仓库获取的隔离 Tag |
| `vendor-X.Y.Z` | 已合入 `main` 的官方基线 |
| `upgrade/vX.Y.Z` | 持久升级分支 |

官方 Tag 和本仓库 Tag 必须保存在不同引用命名空间，不能使用同名本地 Tag 推断
官方来源。

## 3. 启用方式

定时检测默认关闭。完成公开迁移、分支保护和首次手动升级验证后，在仓库变量中设置：

```text
UPSTREAM_SYNC_ENABLED=true
```

未设置时仍可手动运行 `upstream-sync.yml`。

## 4. 检测与准备

同步 Workflow 应：

1. 从 `Wei-Shaw/sub2api` 获取正式 Tag 到隔离引用。
2. 确定当前可达的最新 `vendor-*`。
3. 比较新官方版本和当前二改 Tree。
4. 检查 `.github/upstream-shadowed-sources.tsv`。
5. 识别保护路径、数据库 Migration 和 Schema 变化。
6. 创建或复用 `upgrade/vX.Y.Z`。
7. 保留已有升级现场，不重置、不强推。
8. 创建以 `main` 为 Base 的升级 PR。
9. 从受信任 `main` 调度准确 PR Head SHA 的升级门禁。

## 5. 影子来源和保护路径

二改已从部分官方路径迁到 `backend/internal/custom` 与 `frontend/src/custom`。升级时
必须检查官方影子来源是否发生变化，不能直接把新官方实现覆盖到 Custom 目标。

保护范围至少包括：

- `.github/workflows/`；
- `.github/custom-*` 与影子映射；
- `backend/internal/custom/`；
- `frontend/src/custom/`；
- `deploy/release/`；
- 更新、runtime 与发布薄桥接；
- 数据库边界工具和例外表。

## 6. 数据库门禁

以下变化必须人工评审：

- 新增、删除或重排 Migration；
- Ent Schema、生成 Schema 或数据库初始化语义变化；
- 表、列、索引、约束、默认值和数据回填变化；
- 可能影响回退或滚动升级兼容性的代码；
- 对 `.github/custom-database-exceptions.tsv` 的修改。

评审记录必须说明前向迁移、旧版本兼容、备份、回滚和验证方法。没有结论时不得
自动合并或发布。

## 7. PR 权限边界

`pull_request_target` 只用于从默认分支执行可信 Workflow。候选代码视为不可信：

- 只使用 `contents: read`；
- 不注入 Secrets；
- 不授予 Package、Release 或部署写权限；
- 不从 PR 修改后的 Workflow 获取执行步骤；
- 最终写操作只允许受信任 `main` 的 `workflow_dispatch`；
- 写操作前重新验证 PR 状态、Base、Head SHA、检查结果和祖先关系。

## 8. 完整门禁

升级 PR 至少运行：

- 冲突与保护路径检查；
- 差异台账和 Candidate Tree；
- 影子来源映射检查；
- 数据库语义门禁；
- 后端 Unit、Integration 和生产构建；
- Wire 生成一致性；
- 前端 Typecheck、Custom Vitest 和生产构建；
- GoReleaser 校验和 Release 预构建；
- Action Pin、权限和 Actionlint。

普通 CI 与升级专用门禁分工如下：

- 只有名称精确匹配 `upgrade/vX.Y.Z` 的分支才把最终态差异台账和数据库边界检查
  交给受信任升级门禁；普通分支仍按当前 `vendor-*` 基线执行这两项检查。
- 升级分支的普通 CI 仍必须继续执行供应链、Shell、后端、前端、Lint 和构建检查，
  不能因为处于升级流程而跳过。
- 普通 CI 与升级专用门禁的后端 Lint 都加载
  `.github/custom-upstream-baseline.env`，验证可信基线是当前 Head 的祖先，并使用
  `--new-from-rev "$CUSTOM_UPSTREAM_BASE_COMMIT"` 只阻断基线后的新增问题；基线中
  已存在的官方继承 Lint 问题只记录，不通过修改官方源码来消除。
- 后端 Unit、Integration、Wire 生成一致性、前端完整检查和 Release 预构建仍是
  全量硬门禁，不受增量 Lint 范围影响。
- 升级 PR 必须把 `.github/custom-upstream-baseline.env` 精确滚动到目标
  `vendor-X.Y.Z^{commit}`，同步重建 `.github/custom-upstream-delta.tsv`，并按新
  基线更新 `.github/custom-database-exceptions.tsv` 的只读语义指纹。受信任升级
  门禁只允许这三个滚动文件作为控制面例外，验证目标 Tag、官方 Commit、Candidate
  Tree、每个 Blob 和最终数据库边界后才允许合并；其他工作流、发布脚本和控制面
  修改仍须先独立合入 `main`。
- 数据库审批始终绑定升级前 `main` 中的受信任基线。候选分支的新基线只用于验证
  合并后状态，不能缩小本次官方数据库差异或复用旧 Head 的审批。
- Branch Protection 除普通 CI 检查外，还必须要求
  `Required upgrade validation`。该状态由受信任 `main` Workflow 绑定准确 PR Head
  SHA 发布，升级所需的差异台账、影子来源、保护路径和数据库批准均成功后才为
  `success`。
- Security Scan 保持独立报告，不配置为 Required Check。

## 9. 合并与收尾

门禁通过并完成必要人工批准后：

1. 合并升级 PR 到 `main`。
2. 重新确认合并后的提交包含准确官方 Commit。
3. 将官方 Commit 更新到 `refs/heads/upstream/main`。
4. 创建或验证 `vendor-X.Y.Z` 指向同一官方 Commit。
5. 从受信任 `main` 调度准确合并 SHA 的 CI 与 Security Scan。
6. CI 成功后才允许发布 `vX.Y.Z-custom.1`。

`upstream/main` 与 `vendor-*` 的更新必须是同一次受控收尾，避免产生“代码已合并但
基线未登记”的中间状态。

## 10. 阻断恢复

| 状态 | 处理 |
| --- | --- |
| `conflict` | 在现有升级分支解决并补充测试 |
| `protected_overlap` | 先审查官方变化如何映射到 Custom 路径 |
| `database_review` | 完成人工数据库评审和回滚说明 |
| `tests` | 修复候选分支，不能关闭检查绕过 |
| 收尾失败 | 核对 `main`、`upstream/main`、`vendor-*` 后从可信分支重试 |
| Release/GHCR 失败 | 保持 Tag 不变，按发布恢复流程处理 |

AI 或维护者修复后继续使用同一个 `upgrade/vX.Y.Z`，确保历史和讨论可追溯。

## 11. 升级记录模板

```markdown
# Official upgrade vX.Y.Z

- Official tag and commit:
- Previous vendor baseline:
- Upgrade branch and PR:
- Custom/official overlap:
- Shadow source changes:
- Database review:
- CI and Security Scan:
- Release preflight:
- Rollback notes:
- Final vendor tag:
```

记录不得包含生产凭据、主机信息或真实用户数据。
