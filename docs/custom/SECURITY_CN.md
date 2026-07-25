# 公开仓库安全边界

## 1. 公开不等于公开凭据

源码、Workflow 定义、Actions 日志、Release 和 Issue 都可能被任何人读取。真实
Token、密码、Cookie、数据库、服务器地址和用户数据不得进入任何 Git 对象或公开
GitHub 内容。

GitHub Secrets 的值不会因仓库公开而自动展示，但 Workflow 代码和权限边界会被
公开审查。任何可执行公开 PR 代码的 Job 都不得获得生产 Secrets 或写权限。

## 2. 凭据分类

| 凭据 | 用途 | 保存位置 | 是否进入应用容器 |
| --- | --- | --- | --- |
| `GITHUB_TOKEN` | Actions 当前仓库操作 | GitHub 自动签发 | 否 |
| GHCR 发布权限 | 发布镜像 | Release Job 的 `GITHUB_TOKEN` | 否 |
| GHCR 拉取凭据 | 拉取 Private Package | 宿主机 Credential Store | 否 |
| 可选 Release Token | 提高 GitHub API 限额 | Environment Secret 或只读文件 | 可选 |
| 生产部署凭据 | 连接目标环境 | 受保护 Environment | 仅部署 Job |

不同用途的 Token 不能混用。

## 3. 公开 Release 访问

`o87110/sub2api-custom-public` 的 Release 可匿名查询和下载。正常更新流程不得强制
要求 Token。

如果配置 `UPDATE_GITHUB_TOKEN` 或 `UPDATE_GITHUB_TOKEN_FILE`：

- Token 文件优先于环境变量；
- 只发送到精确 HTTPS `api.github.com`；
- URL 不得包含用户信息、查询凭据或片段；
- 跳转到 GitHub 资产主机前删除 Authorization；
- 文件缺失、为空或不可读时明确失败；
- 日志、错误、缓存、浏览器和 Release 说明不得回显 Token。

## 4. Actions 最小权限

Workflow 顶层默认：

```yaml
permissions: {}
```

按 Job 授予最小权限：

- CI：`contents: read`；
- 状态报告：必要时 `statuses: write` 或 `pull-requests: write`；
- Release 发布：`contents: write`、`packages: write`；
- 失败 Issue：`issues: write`；
- 不需要 OIDC 时不授予 `id-token: write`。

第三方 Action 必须固定完整 Commit SHA。构建下载工具必须固定版本、HTTPS 地址和
SHA256。

## 5. 公共 PR 威胁模型

- `pull_request` 默认使用只读 Token，不能访问仓库 Secrets。
- `pull_request_target` 运行默认分支 Workflow，必须视 PR 内容为不可信输入。
- 带写权限的 Job 不得 Checkout 或执行 PR Head 代码。
- 升级门禁执行候选代码时只能使用只读权限且不注入 Secrets。
- 最终写操作只能从受信任 `main` 的手动调度路径执行，并重新验证 PR、Head SHA、
  基线和检查状态。
- Fork PR 不能直接触发 Release、GHCR 或生产部署。

## 6. Release 供应链

- Tag 不可变；
- 已发布资产不能覆盖或删除后重建；
- Release Manifest 绑定 Commit、Workflow、Artifact 与 OCI Digest；
- 构建和发布权限分离；
- 构建 Job 不接触发布或生产凭据；
- 发布 Job 只处理已验证的构建产物；
- 下载校验大小、SHA256、文件类型、路径和二进制版本；
- GHCR 不使用浮动 `latest`。

## 7. GHCR

仓库公开不会自动公开已有或新建的 GHCR Package。首次发布后检查：

1. Package Visibility 是否为 Public；
2. Package 是否连接到 `o87110/sub2api-custom-public`；
3. Actions Access 是否只授予必要仓库；
4. Fork 和 `pull_request_target` 是否无法取得 `packages: write`；
5. 生产部署是否固定 Digest 或完整版本标签。

## 8. 分支与发布保护

- `main` 禁止 Force Push；
- Workflow 与发布脚本需要 Code Owner Review；
- 自动发布默认关闭，完成保护配置后才启用；
- GitHub Environment 设置最小 Secrets 和 `main`/`v*-custom.*` 部署策略，不设置
  `Required reviewer`；
- 官方 Tag 必须从隔离的上游 Remote 获取；
- `vendor-*` 只能由通过门禁的收尾任务创建。

## 9. 泄露处置

发现敏感信息进入提交、日志、Artifact、Release 或截图时：

1. 立即停止发布和部署。
2. 撤销并轮换相关凭据。
3. 删除仍可删除的公开运行、Artifact 或 Release Payload。
4. 判断内容是否进入 Git 历史、Fork、缓存或第三方克隆。
5. 必要时重写历史并通知已知使用者重新克隆。
6. 修复产生泄露的 Workflow、脚本或日志。
7. 记录不含秘密值的影响范围和恢复证据。

一旦内容公开，应假定已经被复制；删除不能替代凭据轮换。

## 10. 安全报告

公开 Issue 不适合提交未修复漏洞细节、利用代码或真实凭据。优先使用 GitHub
Security Advisory 的 Private Vulnerability Reporting；若未启用，应只提交不含
敏感细节的联系请求，由维护者建立私密沟通渠道。
