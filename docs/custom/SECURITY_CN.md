# 公开仓库安全边界

## 1. 公开不等于公开凭据

源码、Workflow 定义、Actions 日志、Release 和 Issue 都可能被任何人读取。真实
Token、密码、Cookie、数据库、服务器地址和用户数据不得进入任何 Git 对象或公开
GitHub 内容。

`custom-release-publish` Environment 不配置自定义 Secrets，普通自动发布只使用
GitHub 为 Job 临时签发的 `GITHUB_TOKEN`。Repository 可选配置
`UPGRADE_FINALIZER_TOKEN`，仅用于官方 Commit 含 Workflow 变化时更新受控镜像和
Vendor Tag；任何可执行公开 PR 代码的 Job 仍不得获得该 Secret 或写权限。

## 2. 当前权限与运行凭据边界

- CI 使用只读的临时 `GITHUB_TOKEN`，无需维护者创建或保存 Token。
- Release Job 使用同一机制临时取得 `contents: write` 和 `packages: write`，权限不
  进入构建 Job 或应用容器。
- `UPGRADE_FINALIZER_TOKEN` 若配置，必须使用最小仓库范围和更新 Workflow 引用所需
  权限，只注入受信任 `main` 的最终器；候选门禁、构建、发布及
  `pull_request_target` 候选执行 Job 均不得取得。
- 公开 Release 与当前 Public GHCR Package 均支持匿名访问，正常部署不需要 GitHub
  或 GHCR 拉取凭据。
- PostgreSQL、Redis、JWT、TOTP 和管理员密码属于各部署自己的应用运行凭据，不是
  GitHub 发布凭据，必须在仓库外独立生成和保存。

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
- 自动 Release 写操作只接受成功 CI 所绑定的准确 `main` SHA；首次发布和恢复重试
  才使用手动调度。
- 同一不可变 SHA 的成功 CI 与 `boundaries` 证据可复用，但 Release Preflight 必须
  重新验证 Workflow、事件、`main`、Head SHA、结论和唯一边界 Job；不得把复用解释为
  绕过或伪造门禁。
- 官方升级的最终收尾只能从受信任 `main` 手动调度，并重新验证 PR、Head SHA、
  base SHA、merge SHA、基线和检查状态；可选最终器 Token 只能在这些门禁成功后使用。
- Fork PR 不能直接触发 Release 或 GHCR 发布。

## 6. Release 供应链

- Tag 不可变；
- 已发布资产不能覆盖或删除后重建；
- Release Manifest 绑定 Commit、Workflow、Artifact 与 OCI Digest；
- CI 前端 Artifact 还必须绑定准确 Run ID、Workflow、事件、`main`、Head SHA、ID、
  Digest 和内容 SHA256；下载前后限制大小，解压前拒绝路径穿越、重复路径、符号链接、
  特殊文件和额外根目录；
- 前端 Artifact 缺失或过期可从准确 Tag 本地重建；重复、来源错误、结构或 Digest
  异常必须失败关闭，不得借降级绕过；
- 构建和发布权限分离；
- 构建 Job 不注入 Repository 或 Environment 自定义 Secrets，也不持有写权限；
- 发布 Job 只处理已验证的构建产物；
- 下载校验大小、SHA256、文件类型、路径和二进制版本；
- GHCR 不使用浮动 `latest`。
- Buildx GHA Cache 使用固定 scope，只加速构建，不作为提交、Artifact、Manifest、
  Release 资产或 GHCR Digest 的可信证据；缓存故障不能掩盖真实构建失败。

## 7. GHCR

当前 `ghcr.io/o87110/sub2api-custom-public` 已设置为 Public，正常拉取不需要
`docker login`。仓库公开不会自动保证 Package 可见性不发生漂移，因此发布后检查：

1. Package Visibility 是否为 Public；
2. Package 是否连接到 `o87110/sub2api-custom-public`；
3. Actions Access 是否只授予必要仓库；
4. Fork 和 `pull_request_target` 是否无法取得 `packages: write`；
5. 生产部署是否固定 Digest 或完整版本标签。

只有未来明确将 Package 改为 Private 时，宿主机才需要最小 `read:packages` 拉取
凭据；该条件凭据不得传入应用容器。

## 8. 分支与发布保护

- `main` 禁止 Force Push；
- `CODEOWNERS` 标识 Workflow 与发布脚本责任人；当前分支保护不要求人工审批或
  Code Owner Review；
- 自动发布默认关闭，完成保护配置后才启用；
- GitHub Environment 不配置自定义 Secrets，只保留 `main`/`v*-custom.*` 部署
  策略，且不设置 `Required reviewer`；
- 官方 Tag 必须从隔离的上游 Remote 获取；
- `vendor-*` 只能由通过门禁的收尾任务创建。
- 创建 `vendor-*` 前必须临时停用 `release.yml` 并在退出时恢复原 active 状态，防止
  Tag 所指官方 Commit 中宽泛的 `v*` 触发器发布官方资产；原本已停用的状态保持不变。
  恢复后必须短时检查同 Tag、同官方 Commit 的 Push Release；发现后立即取消并失败
  关闭，禁止继续调度生产发布。

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

## 10. 前端依赖审计

- Security Workflow 使用 pnpm 9.15.9 执行 `install --frozen-lockfile`，并对生产依赖
  运行 `pnpm audit --prod --audit-level=high`；审计报告必须先通过结构和退出码校验。
- 高危及严重漏洞优先升级或使用最小范围的条件 Override 修复。只有无法立即升级且已有
  明确缓解措施、负责人和到期日时，才允许登记精确 Advisory 例外。
- `postcss@8.5.23` 间接解析到受 `GHSA-2v37-7h3g-55p8` 影响的
  `nanoid@3.3.16` 时，固定使用 `nanoid@<3.3.17: 3.3.18`。该条件 Override 仅覆盖
  漏洞版本范围，并同步锁文件；不得通过新增审计例外或降低审计级别绕过。
- 依赖修复至少验证 frozen install、生产依赖审计、审计例外校验、前端类型检查、测试
  和生产构建。

## 11. 安全报告

公开 Issue 不适合提交未修复漏洞细节、利用代码或真实凭据。当前仓库尚未启用
GitHub Security Advisory 的 Private Vulnerability Reporting；启用前只能提交不含
敏感细节的联系请求，由维护者建立私密沟通渠道。启用后优先使用该私密报告入口。
