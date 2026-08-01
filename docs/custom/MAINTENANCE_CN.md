# 日常维护与变更记录

## 1. 单一事实源

所有业务源码、文档和 Actions 只在公开仓库 `main` 维护。旧私有仓库是历史归档，
不接收同步，也不能作为发布来源。

本地建议 Remote：

```text
origin    git@github.com:o87110/sub2api-custom-public.git
upstream  https://github.com/Wei-Shaw/sub2api.git
```

## 2. 二改范围

维护二改时重点检查：

```text
backend/internal/custom/
frontend/src/custom/
deploy/release/
.github/custom-*.tsv
.github/custom-*.env
.github/workflows/
docs/custom/
```

官方目录中的薄桥接文件必须保持最小化。新增二改文件或修改分类时，同步更新
`.github/custom-upstream-delta.tsv`、`.github/custom-thin-bridge-contract.tsv` 和必要的
影子来源映射。薄桥契约的增删预算必须由准确 Vendor Commit 与 Candidate Tree 计算，
不能用扩大预算掩盖业务职责回流。`delegate`/`view` 新增函数默认失败，只有已审核的
查询、DTO 或单次委托适配器可以按路径和函数名登记；新增控制流还必须匹配路径、所属
函数和完整语句，新增调用面必须匹配路径、所属函数或模板事件位置、执行目标和精确
次数。门禁须同时覆盖改名、普通条件分支、无控制流多步骤函数，以及 Vendor 已有函数
内直接、括号、可选或计算属性调用和模板事件处理器的负向夹具。

## 3. 开发流程

1. 从最新 `main` 创建功能分支。
2. 先确认需求属于官方行为、二改行为还是部署行为。
3. 优先在 Custom 目录实现，官方目录只做接线。
4. 添加最小、可重复的单元或集成测试。
5. 运行后端、前端、生成代码和边界检查。
6. 更新功能、测试、安全或运维文档。
7. 通过 PR 合入 `main`。

不要在功能 PR 中顺带改版本、移动 Tag、调整包可见性或修改生产 Environment。

## 4. 发布说明

每个 `vX.Y.Z-custom.N` 至少说明：

```markdown
## Custom changes

- 用户可见变化
- 安全或兼容性变化

## Database

- 无数据库变化，或列出 Migration 与回滚条件

## Validation

- CI / Security Scan
- 定向测试
- Release / GHCR 验证
```

公开 Release 说明不得包含内部服务器、真实账号、Token 路径或生产故障细节。
发布控制面必须生成并校验上述三个二级标题各且仅出现一次。自动发布根据前一正式
Custom Tag 与目标提交生成变更比较、Migration/Ent Schema 差异、备份与回滚提示、
精确 SHA CI 链接及 Release/GHCR 验证约束；手动输入只能作为
`Custom changes` 下的附加纯文本条目，不能替代固定章节。

## 5. 文档同步矩阵

| 变化 | 必须更新 |
| --- | --- |
| 新增/删除二改功能 | `FEATURES_CN.md`、`TESTING_CN.md` |
| 更新器、URL 或 Token 语义 | `FEATURES_CN.md`、`SECURITY_CN.md` |
| Release、Tag、GHCR | `REPOSITORY_RELEASE_CN.md` |
| Compose、runtime、回退 | `OPERATIONS_CN.md` |
| 官方升级流程 | `UPSTREAM_SYNC_CN.md` |
| 文件边界 | 差异台账、薄桥契约、影子来源映射 |
| 数据库语义 | 数据库例外表、升级评审记录 |

## 6. 定期检查

### 每次 PR

- Custom 目录边界是否保持；
- 81 个官方薄桥路径是否与契约精确一致，新增 Custom 导入是否有影子映射；
- 是否出现新密钥、绝对路径、服务器地址或生产数据；
- 第三方 Action 是否固定完整提交；
- Workflow 权限是否为最小集合；
- PR 代码是否可能在带写权限或 Secrets 的 Job 中执行。

### 每次发布

- 发布提交是当前 `main` 的已审查提交；
- CI 与 Security Scan 对应准确 SHA；
- `vendor-*` 基线是目标提交祖先；
- Tag 不存在或指向同一不可变提交；
- Release Manifest、校验和和 GHCR Digest 一致；
- 更新、回退和 runtime 持久化完成演练。

### 每月

- 依赖漏洞和例外是否仍有效；
- Actions Pin 和构建工具校验和是否需要升级；
- Repository 和 Environment 自定义 Secrets 是否保持为空，Deploy Key 是否仍有
  用途；
- GHCR 包可见性和 Actions 访问列表是否符合预期；
- 旧 Artifact、缓存和失败运行是否需要按公开策略处理。

## 7. 常见故障

### 匿名更新查询被限流

先检查 GitHub API Rate Limit、代理和 DNS。确需提高限额时配置最小权限 Token，
但公开 Release 的正常下载不得强制依赖 Token。

### GHCR `unauthorized`

当前 GHCR Package 应为 Public，正常拉取无需登录。出现 `unauthorized` 时先按
可见性漂移处理，核对 Package 是否仍为 Public、是否连接到正确仓库，以及 Actions
Access 是否授予 `sub2api-custom-public`；只有明确改为 Private 时才配置宿主机拉取
凭据。

### Release 工作流失败

不要移动 Tag 或覆盖资产。保留失败运行，修复控制面后针对同一不可变标签重试；
若已正式发布但制品不完整，将该版本标记为不可部署并发布下一个修复版本。

### 官方同步被阻断

继续使用现有 `upgrade/vX.Y.Z` 分支，修复冲突、保护路径或数据库评审项。禁止通过
关闭门禁、强推或手工移动 `vendor-*` 绕过。

## 8. 维护记录

公开维护记录只保留可复现信息：提交 SHA、官方 Tag、测试命令、错误码和公开链接。
不得记录真实 Token、密码、Cookie、用户数据或生产地址。
