# 仓库、版本与公开发布流程

## 1. Remote 与分支

```text
origin    = git@github.com:o87110/sub2api-custom-public.git
upstream  = https://github.com/Wei-Shaw/sub2api.git
```

| 分支 | 责任 |
| --- | --- |
| `main` | 二改源码、文档和可信工作流 |
| `upstream/main` | 官方镜像提交，不作为默认分支 |
| `upgrade/vX.Y.Z` | 官方升级冲突和审查现场 |
| 功能分支 | 通过 PR 合入 `main` |

默认分支必须是 `main`。旧私有仓库不参与同步或发布。

## 2. 版本语义

| 类型 | 示例 | 含义 |
| --- | --- | --- |
| 官方 Tag | `v0.1.162` | 官方项目正式版本 |
| Vendor Tag | `vendor-0.1.162` | 已审查并合入二改主线的官方基线 |
| 自定义 Tag | `v0.1.162-custom.1` | 本仓库不可变发布版本 |

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

公开迁移后的自动发布默认关闭。首次发布使用 `workflow_dispatch`；完成分支保护、
GHCR 和 Environment 设置后，再创建仓库变量：

```text
AUTO_PUBLISH_ENABLED=true
```

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

## 5. Release 构建

Release Workflow 分为三个权限隔离阶段：

1. `context`：验证 Tag、提交、CI 和现有 Release 状态。
2. `build`：无发布权限地构建归档、校验和与 OCI Layout，并上传短期 Artifact。
3. `publish`：验证 Artifact 和 Manifest 后创建 Release、上传资产并推送 GHCR。

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

## 8. 分支保护

`main` 至少配置：

- 必须通过 PR；
- 必须通过 CI 与 Security Scan；
- 禁止 Force Push 和删除；
- Workflow、发布脚本和差异台账要求 Code Owner Review；
- 合并前分支必须是最新状态；
- 管理员是否允许绕过应显式决定，默认不绕过。

## 9. 发布失败

- 不移动已有 Tag；
- 不使用 `--clobber` 覆盖资产；
- Draft 或 Artifact 可恢复时针对同一 Tag 重试；
- 正式 Release 已发布但 GHCR 不完整时，将版本视为不可部署；
- 修复控制面后发布下一个 `custom.N`；
- Issue 和日志只记录公开链接、SHA、Digest 与错误码，不记录生产信息。
