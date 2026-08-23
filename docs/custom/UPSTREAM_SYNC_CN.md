# 同步官方版本与升级门禁

## 1. 目标

公开仓库只同步 `Wei-Shaw/sub2api` 的正式 `vX.Y.Z` Tag，不直接跟随官方远程尚未
发布的 `upstream/main`。升级必须保持二改功能、数据库安全、可复现分支和完整
审查证据。

## 2. 引用与分支

| 引用 | 含义 |
| --- | --- |
| `main` | 当前公开二改主线 |
| `upstream/main` | 官方远程尚未审核的最新主线，不作为当前基线 |
| `origin/upstream/main` | 本仓库最近完成审查的官方镜像提交 |
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
5. 用 `.github/custom-thin-bridge-contract.tsv` 复核全部薄桥类型、精确增删预算和
   Custom 目标；不得仅凭差异台账登记判断职责合规。
6. 识别保护路径、数据库 Migration 和 Schema 变化。
7. 创建或复用 `upgrade/vX.Y.Z`。
8. 保留已有升级现场，不重置、不强推。
9. 创建以 `main` 为 Base 的升级 PR。
10. 从受信任 `main` 调度准确 PR Head SHA 的升级门禁。

## 5. 影子来源和保护路径

二改已从部分官方路径迁到 `backend/internal/custom` 与 `frontend/src/custom`。升级时
必须检查官方影子来源是否发生变化，不能直接把新官方实现覆盖到 Custom 目标。

保护范围至少包括：

- `.github/workflows/`；
- `.github/custom-*` 与影子映射；
- `tools/validate_custom_thin_bridges.py` 及其失败夹具；
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
- 全量薄桥契约、精确增删预算和禁止高风险业务符号回流断言；
- `delegate/view` 未登记新函数、改名循环、普通条件分支、Watcher、重试、协调流程和
  无控制流多步骤函数检查，以及精确到路径、所属函数或模板事件位置、完整控制语句和
  新增执行目标/次数的允许结构；
- 影子来源映射检查；
- 数据库语义门禁；
- 后端 Unit、Integration 和生产构建（由可信升级门禁执行一次）；
- Wire 生成一致性；
- 前端 Typecheck、Custom Vitest 和生产构建（由可信升级门禁执行一次）；
- GoReleaser 校验和 Release 预构建；
- Action Pin、权限和 Actionlint。

普通 CI 与升级专用门禁分工如下：

- 只有名称精确匹配 `upgrade/vX.Y.Z` 的分支才把最终态差异台账和数据库边界检查
  交给受信任升级门禁；普通分支仍按当前 `vendor-*` 基线执行这两项检查。
- 升级分支的普通 CI 只执行快速结构检查；完整后端、前端和适用 Release 预构建由
  可信升级门禁执行一次，避免双轨重复测试。
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

普通升级 PR 不再因标签变化触发验证，也不重复执行普通 CI 的完整后端、前端和 Lint。
Release preflight 仅在 Dockerfile、GoReleaser、Release 脚本、Go 依赖、前端依赖或服务
版本文件变化时启动；其他升级仍由同一次可信升级门禁完成边界、后端和前端验证。
上游定时检查默认每 6 小时一次，手工调度仍可用于即时检查和失败重试。

## 9. 合并与收尾

门禁通过并完成必要人工批准后：

1. 合并升级 PR 到 `main`。
2. 重新确认合并后的提交包含准确官方 Commit。
3. 将官方 Commit 更新到 `refs/heads/upstream/main`。
4. 创建或验证 `vendor-X.Y.Z` 指向同一官方 Commit。
5. 从受信任 `main` 调度准确合并 SHA 的 CI 与 Security Scan。
6. CI 成功后才允许发布 `vX.Y.Z-custom.1`。

`refs/heads/upstream/main` 与 `vendor-*` 的更新必须由同一次受控收尾负责。官方
Commit 可能包含 Workflow 文件变化，临时 `GITHUB_TOKEN` 不通过 Git Push 上传这类
对象；最终器应在确认对象已经随已合并 PR 进入仓库后，使用 Git Database API 先做
镜像分支的非强制快进，再创建指向同一 Commit 的不可移动注释 Tag，并重新抓取验证
分支、Tag 类型和 peeled Commit。任何一步失败都不得移动已有 Tag。

若希望这类升级也全自动收尾，可配置 Repository Secret
`UPGRADE_FINALIZER_TOKEN`，其权限只用于更新包含 Workflow 变化的受控引用。Secret
仅注入从受信任 `main` 手动调度、且全部候选门禁已成功的最终器，不进入候选代码、
构建、测试或发布 Job。未配置时，最终器必须在合并前识别 Workflow 差异并失败关闭；
维护者可在门禁证据仍精确绑定时通过本机受信任 SSH 恢复引用，再运行显式恢复模式。

若 PR 已合并但上述引用或精确 SHA 调度未完成，只允许从受信任 `main` 显式启用
`resume_finalization`。恢复任务必须重新绑定原 PR 的 base SHA、Head SHA 和 merge SHA，
确认三者仍在当前 `main` 历史中，重跑同一候选门禁，并仅补齐缺失引用或调度。镜像已
快进但 Tag 尚未创建、或 Tag 已正确创建但后续调度失败，均按相同验证规则恢复；不同
目标或轻量 `vendor-*` Tag 必须失败关闭。

创建新的 `vendor-*` Tag 前，最终器必须读取仓库 `release.yml` 的当前状态。若其处于
active，先通过 Actions API 临时停用，再创建注释 Tag，并在成功路径和异常退出路径
恢复原状态。原因是官方 Workflow 的 `v*` 模式也会匹配 `vendor-*`，而 Tag 事件读取
的是 Tag 所指官方 Commit 中的 Workflow；仅收紧 `main` 上的 Custom Tag 模式不能阻止
该官方 Release 误触发。原本已停用的 Workflow 不得被最终器擅自启用。创建 Tag 后
必须等待事件收敛再恢复 Workflow，并短时轮询同 `vendor-*`、同官方 Commit 的 Push
Release；若仍因竞态出现，先取消运行再失败关闭，只允许通过精确最终器恢复继续。

## 10. 阻断恢复

| 状态 | 处理 |
| --- | --- |
| `conflict` | 在现有升级分支解决并补充测试 |
| `protected_overlap` | 先审查官方变化如何映射到 Custom 路径 |
| `database_review` | 完成人工数据库评审和回滚说明 |
| `tests` | 修复候选分支，不能关闭检查绕过 |
| 收尾失败 | 核对 `main`、`origin/upstream/main`、`vendor-*` 后，从可信 `main` 对准确已合并 PR 启用 `resume_finalization` |
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
