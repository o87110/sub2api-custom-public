# 测试与验收矩阵

## 1. PR 基础门禁

每个 PR 至少验证：

| 领域 | 验证 |
| --- | --- |
| 后端 | Unit、Integration、生产构建 |
| 前端 | Typecheck、Custom Vitest、生产构建 |
| 生成代码 | `go generate ./cmd/server` 后无差异 |
| Lint | 仅检查相对显式官方基线的新问题 |
| 差异边界 | Candidate Tree 与差异台账一致 |
| 数据库 | Migration、Schema 和例外表语义门禁 |
| Actions | Action Pin、权限和 Actionlint |
| 安全 | `govulncheck` 与生产依赖审计 |

## 2. cyber_policy

### 范围内

- 官方处罚、通知、计数和会话屏蔽保持一致；
- 并发命中不会丢失或重复累计；
- 管理页面状态和后端配置一致。

### 范围外

- 记录 `cyber_policy_out_of_scope`；
- 不通知、不累计、不封号、不写会话屏蔽；
- 日志摘要不包含 Token、密钥、完整 URL 或完整请求体。

### 动态变化

- 分组加入审计范围后下一请求进入官方处罚；
- 移出后下一请求进入范围外逻辑；
- 多实例配置刷新后行为一致。

## 3. 公开 Release 更新

### 来源

- 更新源固定为 `o87110/sub2api-custom-public`；
- 请求参数、环境变量和缓存不能切换仓库；
- 未配置 Token 时可匿名读取 Release 与资产；
- 可选 Token 只发送到受信任 GitHub API，重定向后被剥离；
- 自定义源失败不能安装官方资产。

### 下载与安装

- 只接受合法 `vX.Y.Z-custom.N`；
- 拒绝非 HTTPS、错误仓库、查询参数、片段和未批准主机；
- 强制最大尺寸与 SHA256；
- 拒绝路径穿越、符号链接和额外文件；
- `--version` 与目标 Tag 不一致时拒绝替换；
- 替换失败恢复原二进制和备份。

### 缓存与版本

- 缓存绑定仓库和完整版本；
- 跨仓库、过期、旧格式和污染缓存被拒绝；
- `has_update` 使用完整自定义版本；
- `has_official_update` 使用基础版本；
- 官方查询警告不覆盖自定义 Release 信息。

## 4. runtime 持久化

- 空 runtime 从镜像基线初始化；
- 合法 runtime 在容器重建后保持；
- 目录、符号链接、空文件、错误权限和版本不匹配被拒绝；
- 更新后 `/proc/1/exe` 指向 `/app/runtime/sub2api`；
- 重建前后 SHA256 一致；
- `.backup` 回退路径通过故障注入验证。

## 5. Release 与 GHCR

- Tag 指向预期 `main` 提交且不可变；
- 最新接受的 CI 和 `boundaries` Job 成功；
- Build Job 无发布权限；
- Artifact ID、Digest、Manifest 和文件 SHA256 一致；
- Release 资产集合精确且无重复；
- OCI Index 包含 `amd64` 与 `arm64`；
- GHCR 多架构及架构标签解析到预期 Digest；
- 不产生 `latest` 或其他浮动标签；
- 已发布资产不能覆盖；
- Release 或 GHCR 任一不完整时版本不可部署。

## 6. 公共 PR 与权限

- Fork PR 只有只读权限且没有 Secrets；
- `pull_request_target` 不执行带写权限的 PR Head 代码；
- 升级候选代码只在只读、无 Secrets Job 中运行；
- 最终收尾只允许受信任 `main` 的手动调度；
- Workflow 文件被候选分支修改时拒绝可信调度；
- Release、Package 和 Environment 权限不能由 PR 输入扩大。

## 7. 官方升级

- 官方 Tag 从 `Wei-Shaw/sub2api` 隔离引用解析；
- `upgrade/vX.Y.Z` 必须基于最新 `main`；
- 工作流、差异台账和保护路径变化被识别；
- 数据库变化必须产生人工评审；
- 冲突分支保留，不重置、不强推；
- 后端、前端、Release 预构建全部通过；
- 合并后准确更新 `upstream/main` 与 `vendor-X.Y.Z`；
- 准确合并 SHA 的 CI 完成后才允许发布 `custom.1`。

## 8. 同机演练

- 使用独立 PostgreSQL、Redis、数据目录、runtime 和端口；
- 默认只绑定 `127.0.0.1`；
- 公开绑定必须设置双重确认；
- 不导入生产账号、API Key、邮件或真实用户数据；
- 使用准确 GHCR Tag 或 Digest；
- 匿名 Release 更新、升级和回退正常；
- `docker compose down -v` 不作为普通清理步骤。

## 9. 验收记录

```markdown
# vX.Y.Z-custom.N 验收

- Source commit:
- Vendor baseline:
- CI run:
- Security Scan run:
- Release manifest SHA256:
- GHCR index digest:
- Database changes:
- Update/rollback rehearsal:
- Known limitations:
```

记录只使用公开 SHA、Digest、运行链接和脱敏错误码，不包含凭据或生产环境信息。
