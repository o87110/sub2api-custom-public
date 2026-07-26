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

功能分支只通过 `pull_request` 运行 CI 与 Security Scan，避免同一提交再由 `push`
重复执行；`push` 仅验证合并后的 `main` 精确提交，自动 Publish 也只接收该分支的
CI 完成事件。同一 PR 推送新提交时取消旧提交仍在运行的 CI 与 Security Scan；
`main` Push、定时任务和可信手工调度使用独立并发键，不得互相取消。

后端 Unit、Integration、Wire/生产构建分为三条并行路径，现有必需检查 `test`
作为失败关闭的聚合门禁；任一路径失败、取消或跳过时，`test` 都不得成功。

公开迁移后的自动发布默认关闭。首次发布使用 `workflow_dispatch`；完成分支保护、
GHCR 和 `custom-release-publish` Environment 设置后，再创建仓库变量：

```text
AUTO_PUBLISH_ENABLED=true
```

`custom-release-publish` Environment 只保留 `main`/`v*-custom.*` 部署策略，
不设置 `Required reviewer`；变量启用后，CI 通过即自动完成 Tag、构建和发布。
普通 `main` Push 由 CI 完成事件触发发布。官方升级最终器使用仓库
`GITHUB_TOKEN` 显式调度绑定合并 SHA 的 Publish；Publish 必须等待同一 SHA 的
CI 和 `boundaries` Job 成功后才能创建 Tag。两条路径共享同一并发组；若重复调度，
已存在的同提交正式 Release 必须安全退出，不得递增出重复版本。

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

1. `context`：验证 Tag、提交、CI 和现有 Release 状态。
2. `build`：无发布权限地构建归档、校验和与 OCI Layout，并上传短期 Artifact。
3. `publish`：进入受 `custom-release-publish` Environment 部署策略约束的发布
   Job，验证 Artifact 和 Manifest，再自动创建 Release、上传资产并推送 GHCR。

构建阶段不持有 `packages: write`，发布阶段不重新执行仓库业务脚本。第三方工具
使用固定版本和 SHA256，Action 使用完整提交 SHA。

## 6. Release Manifest

Manifest 至少绑定：

- Tag Ref OID 与目标提交；
- Workflow Commit、Run ID 与 Attempt；
- Payload Artifact ID 与 Digest；
- 每个归档和 `checksums.txt` 的 SHA256；
- OCI Index Digest、Media Type 与架构 Manifest。

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
