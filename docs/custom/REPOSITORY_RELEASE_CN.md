# 仓库、版本与公开发布流程

## 1. Remote 与分支

```text
origin    = git@github.com:o87110/sub2api-custom-public.git
upstream  = https://github.com/Wei-Shaw/sub2api.git
```

| 分支 | 责任 |
| --- | --- |
| `main` | 二改源码、文档和可信工作流 |
| `refs/heads/upstream/main` | 本仓库已审核官方镜像，本地查看为 `origin/upstream/main` |
| `upgrade/vX.Y.Z` | 官方升级冲突和审查现场 |
| 功能分支 | 通过 PR 合入 `main` |

默认分支必须是 `main`。旧私有仓库不参与同步或发布。

## 2. 版本语义

| 类型 | 示例 | 含义 |
| --- | --- | --- |
| 官方 Tag | `v0.1.164` | 官方项目正式版本 |
| Vendor Tag | `vendor-0.1.164` | 已审查并合入二改主线的官方基线 |
| 自定义 Tag | `v0.1.164-custom.1` | 本仓库不可变发布版本 |

同一官方基线下，每个有效二改发布递增 `custom.N`。Tag 必须为 Annotated Tag，
并指向已通过 CI 的 `main` 提交。

## 3. Actions 分工

| Workflow | 责任 |
| --- | --- |
| `backend-ci.yml` | 后端、前端、生成代码、差异与数据库边界 |
| `security-scan.yml` | Go 漏洞扫描、前端依赖审计 |
| `publish-custom.yml` | 选择下一不可变标签并调度 Release |
| `release.yml` | 构建、验证并发布 Release 与 GHCR |
| `upstream-sync.yml` | 检测官方 Tag、准备升级分支和 PR |
| `upstream-upgrade-gate.yml` | 升级门禁与基线收尾 |

功能分支只通过 `pull_request` 运行分层 CI：`pr-validation` 先做边界、脚本和受影响
路径的快速检查；未知路径、数据库、Ent、订单、支付、库存、鉴权和核心服务变更自动
回退完整检查。Security Scan 不再逐个 PR 运行，只在 `main` Push、每周定时和可信手工
调度运行。`main` Push 运行完整回归并产生稳定的 `full-validation` 状态；自动 Publish
只接收该状态成功的准确 `main` SHA。同一 PR 推送新提交时取消旧 CI。

后端 Unit、Integration、Wire/生产构建分为并行路径，`full-validation` 作为失败关闭的
完整回归门禁；任一路径失败、取消或跳过时不得成功。差异/数据库 `boundaries`、快速
后端/前端、Lint 和 Shell 在准确 SHA 解析后并行；
`boundaries` 仍是独立必需检查。Integration 通过登记表覆盖全部带
`//go:build integration` 的包，新增标签包未登记即失败，不通过 `./...` 重跑普通测试。

公开迁移后的自动发布默认关闭。首次发布使用 `workflow_dispatch`；完成分支保护、
GHCR 和 `custom-release-publish` Environment 设置后，再创建仓库变量：

```text
AUTO_PUBLISH_ENABLED=true
```

`custom-release-publish` Environment 只保留 `main`/`v*-custom.*` 部署策略，
不设置 `Required reviewer`；变量启用后，只有 `full-validation` 成功的 CI 才能自动
完成 Tag、构建和发布。
普通 `main` Push 由 CI 完成事件触发发布。发布前必须确认 CI 事件为 `main` Push 或可信
`workflow_dispatch`、Head SHA 与目标提交一致、Workflow 整体成功，且 `Full validation`
Job 成功。官方升级最终器使用仓库 `GITHUB_TOKEN` 显式调度绑定合并 SHA 的 Publish；
Publish 必须等待同一 SHA 的 CI、`boundaries` 和 `Full validation` Job 成功后才能创建
Tag。两条路径共享同一并发组；若重复调度，
已存在的同提交正式 Release 必须安全退出，不得递增出重复版本。

自动 `workflow_run` 以同一 Vendor 版本的最新正式 Custom Tag 为基准，只在以下
Release 输入变化时创建新 Tag：`backend/**`、`frontend/**`、法律文档、运维归档、
容器入口、Release Dockerfile/构建脚本、工具安装脚本、GoReleaser 配置、工具版本、
Vendor 基线、根 Makefile 或 LICENSE。普通文档、测试和 Workflow 单独变化时成功
跳过；基准不是当前 `main` 祖先时失败关闭。首次 Custom Release、Vendor 基线升级、
未完成 Tag 重试和可信 `workflow_dispatch` 不执行该跳过逻辑。

升级 PR 已合并但 Vendor 基线或发布调度中断时，最终器只能在显式恢复模式下按原
base/Head/merge SHA 重放门禁。官方镜像与注释 `vendor-*` Tag 通过 Git Database API
补齐，以兼容官方 Commit 含 Workflow 变化而临时 Token 不具备 Git Push workflow
权限的情况；全自动引用写入使用仅限受信任最终器的可选 Repository Secret
`UPGRADE_FINALIZER_TOKEN`。未配置且检测到 Workflow 差异时必须在合并前失败关闭；
已有 Tag 只允许精确验证，禁止覆盖或移动。

最终器创建 `vendor-*` 时必须临时停用仓库的 `release.yml`，并用退出 Trap 恢复其原
active 状态。官方 Tag Workflow 可能使用宽泛的 `v*` 触发器，而 `vendor-*` 同样以
字母 `v` 开头；不能依赖 Custom `main` 中较严格的 Tag 过滤规则。若 Workflow 原本已
被人工或闲置策略停用，则保持停用，不得自动启用。创建 Tag 后还必须保留短暂的事件
收敛窗口；恢复 Workflow 后若发现绑定该 `vendor-*` 和官方 Commit 的 Push Release
运行，立即取消并失败关闭，后续只能走精确最终器恢复。

官方同步由独立变量控制：

```text
UPSTREAM_SYNC_ENABLED=true
```

## 4. 发布前置条件

发布目标必须同时满足：

1. 是当前 `main` 的准确提交。
2. 最新接受的 CI 运行成功，且 `boundaries` Job 成功。
3. 当前可达 `vendor-*` 与提交的显式基线一致。
4. 对应官方 Tag 从隔离的上游引用解析。
5. 不存在指向其他提交的同名 Tag。
6. 已有最高 `custom.N` 若未完成发布，优先恢复或重试，不跳号掩盖失败。

Security Scan 独立报告，不能由自动发布工作流伪造或绕过。
每个新 Tag 的注释和 Release Body 必须包含 `Custom changes`、`Database`、
`Validation` 三个固定章节。数据库章节至少列出相对上一正式 Custom Tag 的
Migration 和 Ent Schema 变化、备份要求、回滚评审链接，并明确
`CREATE INDEX CONCURRENTLY` 的事务与失败清理边界。

## 5. Release 构建

Release Workflow 分为三个权限隔离阶段：

1. `context`：验证 Tag、提交、准确 CI、`boundaries` 和现有 Release 状态；同一不可变
   SHA 的成功 CI 证据可复用，不重复 Candidate Tree、差异台账和数据库门禁。
2. `build`：无发布权限地构建归档、校验和与 OCI Layout，并上传短期 Artifact。
3. `publish`：进入受 `custom-release-publish` Environment 部署策略约束的发布
   Job，验证 Artifact 和 Manifest，再自动创建 Release、上传资产并推送 GHCR。

构建阶段不持有 `packages: write`，发布阶段不重新执行仓库业务脚本。第三方工具
使用固定版本和 SHA256，Action 使用完整提交 SHA。

成功的 `main` Push 或可信手工 CI 上传 `release-frontend-dist-<exact-sha>`，保留
30 天，PR 不上传。Release 只从 Preflight 返回的准确 CI Run ID 查询唯一 Artifact，
验证 Workflow、事件、分支、Head SHA、成功状态、Artifact ID/Digest、压缩包 SHA256、
路径边界、文件类型和解压大小。Artifact 不存在或过期时从准确 Tag 本地重建；重复、
来源错误、Digest 不一致、路径穿越、符号链接或额外根目录均失败关闭，不降级。

OCI 构建使用固定 `sub2api-release-oci-v1` GHA Cache scope 和 `mode=max`。缓存是纯性能
优化，不参与信任判断；缓存不可用时冷构建，实际构建失败仍必须失败。Step Summary
分别记录前端准备、GoReleaser、OCI、Artifact 上传和发布耗时。

## 6. Release Manifest

Manifest 至少绑定：

- Tag Ref OID 与目标提交；
- Workflow Commit、Run ID 与 Attempt；
- Payload Artifact ID 与 Digest；
- `build_inputs.frontend` 的模式、来源提交和内容 SHA256；Artifact 模式还包含准确
  CI Run ID、Artifact ID 与 Digest；
- 每个归档和 `checksums.txt` 的 SHA256；
- OCI Index Digest、Media Type 与架构 Manifest。

Schema 继续为 `sub2api-custom-release/v1`。历史 v1 Manifest 允许缺少
`build_inputs.frontend`；所有新 Manifest 必须包含并与 Payload Metadata 完全一致。

正式发布前，远程 Tag、Artifact、Release Asset 和 GHCR 状态必须重新校验。已有
正式资产不得覆盖，Draft 中存在重复或未声明资产时必须失败关闭。

## 7. GHCR

镜像仓库：

```text
ghcr.io/o87110/sub2api-custom-public
```

标签：

```text
vX.Y.Z-custom.N
vX.Y.Z-custom.N-amd64
vX.Y.Z-custom.N-arm64
```

不发布 `latest`、主版本或次版本浮动标签。Package Visibility 与仓库 Visibility
独立；首次发布后必须核对包是否为 Public，并限制 Actions Access 只授予本仓库。
首次发布查询尚不存在的 GHCR 仓库时，仅允许把固定 ORAS 版本返回的完整、精确
`NAME_UNKNOWN` 错误视为空仓库；鉴权、网络、格式或其他查询失败仍必须失败关闭。

## 8. 分支保护

`main` 至少配置：

- 必须通过 PR；
- 必须通过 CI；Security Scan 独立报告且不得配置为 Required Check；
- 禁止 Force Push 和删除；
- `CODEOWNERS` 标识 Workflow、发布脚本和差异台账的责任人，但当前不要求人工审批
  或 Code Owner Review；
- 合并前分支必须是最新状态；
- 必须解决全部 Review Conversation；
- 分支保护同样约束管理员，不允许绕过。

## 9. 发布失败

- 不移动已有 Tag；
- 不使用 `--clobber` 覆盖资产；
- Draft 或 Artifact 可恢复时针对同一 Tag 重试；
- 正式 Release 已发布但 GHCR 不完整时，将版本视为不可部署；
- 修复控制面后发布下一个 `custom.N`；
- Issue 和日志只记录公开链接、SHA、Digest 与错误码，不记录生产信息。
