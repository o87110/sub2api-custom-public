# Sub2API Custom 公开维护手册

本文档是 `o87110/sub2api-custom-public` 的二改入口。仓库基于
[`Wei-Shaw/sub2api`](https://github.com/Wei-Shaw/sub2api)，遵循 LGPL-3.0，
公开维护源码、需求、测试、构建与发布流程。

## 当前基线

| 项目 | 值 |
| --- | --- |
| 公开仓库 | `o87110/sub2api-custom-public` |
| 二改主分支 | `main` |
| 官方远程 | `https://github.com/Wei-Shaw/sub2api.git` |
| 官方基线 | `vendor-0.1.165^{commit}` |
| 基线提交 | `e9a58c1cb8b5ef626a75c93b4d953fde5e67aa29` |
| 已审核官方镜像 | `origin/upstream/main`（远程分支 `refs/heads/upstream/main`） |
| 升级分支 | `upgrade/vX.Y.Z` |
| 发布标签 | `vX.Y.Z-custom.N` |

公开仓库使用官方 `v0.1.162` 的公开历史作为共同祖先，将原私有仓库中当前有效
的二改结果压缩为一次公开迁移提交。原私有提交、Release、Actions、Artifact、
Issue 和生产资料不迁移。

## 二改历史如何保留

干净历史不复制原私有提交，但不会丢失当前有效需求：

1. `docs/custom/FEATURES_CN.md` 记录功能目标和不变量。
2. `.github/custom-upstream-delta.tsv` 记录相对官方基线的每个文件级决定、Blob、
   分类和验证方式。
3. `.github/upstream-shadowed-sources.tsv` 记录从官方实现迁出的二改路径。
4. `docs/custom/TESTING_CN.md` 记录回归和发布验收条件。
5. `openspec/changes/` 保留适合公开的需求、设计、任务和验证证据。
6. `.github/upgrades/` 保留已经完成的官方升级评审记录。

已经废弃、回滚或只与私有运行环境有关的过程记录不进入公开仓库；其最终有效
行为由源码和测试决定。

## 代码边界

新增二改优先放在明确的 Custom 目录：

```text
backend/internal/custom/
├── databaseboundary/       数据库与迁移边界检查
├── moderation/             cyber_policy 范围隔离与摘要
├── paymentchannels/        用户级支付渠道选项、个性化配置与合法组合
└── updater/                自定义 Release、更新与回退

frontend/src/custom/
├── api-keys/               API 密钥当前页批量分组、启用、禁用与删除
├── moderation/             风控二改页面与文案
├── payment-channels/       支付渠道归一化、后台配置、选择器与备用提示
└── updater/                版本、更新与回退界面
```

官方目录只保留必要的薄桥接和 Wire 接线。若修改官方文件，必须在差异台账中说明
原因，并用定向测试证明没有把可隔离逻辑重新散落回官方实现。

## 文档导航

- [代码代理项目级协作规则](../AGENTS.md)
- [二改功能清单](custom/FEATURES_CN.md)
- [日常维护](custom/MAINTENANCE_CN.md)
- [公开部署与运维](custom/OPERATIONS_CN.md)
- [仓库与发布](custom/REPOSITORY_RELEASE_CN.md)
- [安全边界](custom/SECURITY_CN.md)
- [测试与验收](custom/TESTING_CN.md)
- [同步官方版本](custom/UPSTREAM_SYNC_CN.md)

## 必须保持的约束

1. `main` 是唯一二改事实源，不维护双向同步分支。
2. 官方 Tag 只从 `Wei-Shaw/sub2api` 获取，并保存到隔离引用。
3. 在线更新源固定为 `o87110/sub2api-custom-public`，不能由请求参数切换。
4. Release 标签不可变；已发布资产不能覆盖。
5. 构建版本使用完整 `X.Y.Z-custom.N`，官方版本提示使用基础版本。
6. 下载必须校验 SHA256，跨主机重定向必须剥离 Authorization。
7. Repository 和 Environment 不配置自定义 Secrets，构建、测试和 PR 不持有发布
   写权限。
8. GitHub Actions 使用最小权限和固定提交版本的第三方 Action。
9. 数据库迁移或语义变化必须经过数据库门禁和人工审查。
10. 文档不得包含真实 Token、密码、Cookie、服务器地址或生产数据。

## 变更完成定义

一次二改变更至少需要：

- 功能代码与定向测试同步完成；
- 后端、前端和生成代码检查通过；
- 差异台账及影子路径映射保持一致；
- 涉及部署、更新、版本或权限时同步更新对应文档；
- CI 通过；Security Scan 独立运行并如实记录，不作为发布硬门禁；
- 发布前确认准确提交、标签和官方基线关系。

公开迁移后的首次 Release 应使用手动工作流。完成分支保护、GHCR 可见性和
`custom-release-publish` Environment 的 `main`/`v*-custom.*` 部署策略配置，
且不设置 `Required reviewer` 后，才启用自动发布变量。
