# Sub2API Custom 项目级协作规则

## 适用范围

本文件适用于整个仓库。它为 Codex 和其他代码代理提供持久的项目约束；详细设计、
测试、发布和运维规则以 `docs/CUSTOM_DEVELOPMENT_CN.md` 及 `docs/custom/` 为准。
如用户在当前任务中给出更具体的要求，以用户明确要求为准。

## 核心目标

只保留和修改二改需求本身，以及保障二改正常运行、升级、构建、测试、发布和运维
所必需的内容。默认不修复、不重构、不格式化官方继承代码，也不为了让检查结果更
漂亮而改变官方行为。

坚持先检查事实再修改，并遵循最小侵入、KISS、YAGNI 和 DRY。不得顺带处理与当前
请求无关的问题。

## 任务授权边界

- “检查”“审查”“诊断”“说明状态”只授权只读调查和报告，不自动授权修复。
- 用户要求修改或修复时，只改与该请求直接相关的最小文件集合。
- 未经用户明确确认，不执行 `git add`、`git commit`、`git push`、创建或切换分支、
  创建 PR、移动或创建 Tag、创建 Release、推送镜像或触发发布工作流。
- 不操作生产服务器、生产数据库、生产 Secrets、GitHub Ruleset 或 Environment，
  除非用户明确把对应外部操作纳入当前任务。
- 工作区可能包含用户的未提交修改；不得覆盖、回滚、清理或混入无关变更。

## 官方基线与还原原则

- 当前官方基线以 `.github/custom-upstream-baseline.env` 中的
  `CUSTOM_UPSTREAM_BASE_REF` 和 `CUSTOM_UPSTREAM_BASE_COMMIT` 为唯一事实来源，
  不在代码代理规则中硬编码某个旧版本。
- `vendor-0.1.162` 是公开迁移的历史起点；升级记录、测试夹具或历史说明中的该值
  不得误改为当前基线。
- 不属于二改需求的差异应恢复为当前 Vendor 基线，并使用文件内容或 Git Blob
  验证完全一致。
- 官方自身已有的 Bug、测试缺陷、依赖告警、代码风格和实现问题属于官方继承问题，
  除非用户明确要求同步官方修复，否则只记录，不修改。
- 官方测试与官方实现不一致时，记录为继承问题；不得修改官方测试掩盖差异。
- 进行完整对比审查时，必须基于准确 Vendor Commit 分类全部差异，不得仅比较文件名
  或相信自动合并结果。

## 二改代码边界

二改业务实现优先位于：

- `backend/internal/custom/`
- `frontend/src/custom/`

默认不直接修改官方业务实现。必须接入官方代码时，只允许保留最薄的 bridge、
DTO/接口适配、路由注册、Wire 注入、组件导入或启动入口。Bridge 不承载业务编排。

工作流、Dockerfile、`deploy/`、Migration 和生成文件等具有固定工具路径的内容可以
在原位置维护，但只保留二改运行所必需的最小差异。

每次修改必须维护以下边界：

- 所有相对 Vendor 基线保留的差异登记到 `.github/custom-upstream-delta.tsv`，并更新
  到候选树中的准确 Blob；不得以跳过台账测试代替登记。
- 所有“官方源文件 → 二改实际实现”关系登记到
  `.github/upstream-shadowed-sources.tsv`。
- 官方影子源变化时，即使 Git 无文本冲突，也必须进行语义移植审核。
- 官方源文件只保留必要薄接入点；业务逻辑应移入 custom 目录。

## 固定公开 Release 更新边界

- 安装和回退源固定为 `o87110/sub2api-custom-public` 的
  `vX.Y.Z-custom.N` Release。
- 官方 `Wei-Shaw/sub2api` 只用于展示官方版本信息和受控上游同步，不得成为二改安装、
  回退或资产下载来源。
- 公开 Release 默认支持匿名访问。`UPDATE_GITHUB_TOKEN` 和
  `UPDATE_GITHUB_TOKEN_FILE` 只是可选 GitHub API 凭据；未配置 Token 不应失败。
- 配置 Token 文件后，读取、空值或安全校验失败必须明确失败，不得静默降级到环境
  变量；Token 不得出现在日志、错误、前端响应或重定向请求中。
- 仓库、Tag、Release 资产、checksum、版本或来源校验失败时必须失败关闭，不得回退
  安装官方资产。
- 检查命令注入、路径穿越、符号链接、任意文件覆盖、缓存隔离、敏感信息泄露及下载
  大小限制。
- Runtime、镜像基线、legacy image 和 backup 在任何读取、执行、复制或替换前都要
  遵守路径与符号链接边界。
- 持久化 Runtime 若是官方版本或不可信版本，应恢复受信任二改版本；合法且更高的
  二改 Runtime 应保留。本地无 `-custom.N` 的开发构建仍需可用。

## 构建、发布与安全门禁

必须保持以下硬门禁：

- CI、二改边界、差异台账或影子映射检查失败；
- 存在未解决冲突；
- 必需测试、lint、typecheck、编译或构建失败；
- Release 资产、checksum、Manifest、Tag 或目标提交校验失败；
- 官方升级涉及 Migration、Schema、实体字段或 SQL 变化但未经人工审核。

发布只能针对通过 CI 的 `main` 精确提交 SHA。Tag 使用
`vX.Y.Z-custom.N` 且不可变；已有 Tag 不得移动，失败时只能针对原 Tag 恢复或重试。
同一不可变 SHA 的成功 CI 与 `boundaries` 结果可作为 Release 的可信证据复用，
Release 不重复构建 Candidate Tree、差异台账或数据库门禁；但 Preflight 必须重新精确
校验 CI Workflow、事件、分支、Head SHA、结论和唯一的 `boundaries=success`。
Release Notes 必须通过安全的环境变量、文件或标准输入传递，不能直接插值进 Shell。
并且必须包含 `Custom changes`、`Database`、`Validation` 三个固定章节。发布前验证
必要架构资产、checksum、Manifest 和 GHCR Digest。

Security Scan 独立运行并如实报告，不作为自动发布硬门禁，也不得伪装成成功。仓库
当前的自动发布在 CI 通过后运行；官方升级最终器必须显式调度绑定合并 SHA 的发布
等待任务，由其复核同一 SHA 的 CI 与 `boundaries`。`custom-release-publish`
Environment 只保留部署分支/Tag 策略，不设置 `Required reviewer`。修改代码时不得新增
`bypass_checks`、`break-glass` 或其他绕过必需 CI 的入口。

CI 中 `boundaries` 与后端、前端、Lint、Shell 检查在准确目标解析后并行，Workflow
结论、分支保护和 Release Preflight 仍必须要求边界检查成功。Integration 只运行所有
已登记且实际包含 `//go:build integration` 测试的包；新增标签包未登记必须失败，不得
借 Integration 再次执行 `./...` 普通测试。

自动发布只在相对同一 Vendor 版本最新正式 Custom Tag 的 Release 输入变化时创建新
Tag；首次发布、Vendor 基线升级、未完成 Tag 恢复和可信手工调度始终继续。CI 前端
Artifact 只能复用准确成功的 `main` CI Run 与 SHA，并将来源写入 Payload 和 Manifest；
缺失或过期可在准确 Tag 本地重建，重复、来源或 Digest 异常必须失败关闭。Buildx
缓存只用于加速，不参与任何提交、Artifact、Manifest、资产或 OCI Digest 信任判断。

## 数据库边界

- 对比 Vendor 基线与候选树中的 Migration、Schema、实体字段和 SQL。
- 没有明确二改数据库需求时，数据库结构应与官方基线一致。
- 自动流程和本地审查不得对生产数据库执行 `ALTER`、`INSERT`、`UPDATE`、`DELETE`
  或迁移。
- 发现数据库结构变化时，停止自动发布结论并明确报告风险和所需人工审核。

## 文档边界

保留的二改行为必须同步到 `docs/CUSTOM_DEVELOPMENT_CN.md` 或 `docs/custom/` 中对应的
功能、测试、发布、安全、运维或上游同步文档。

不要修改官方 `DEV_GUIDE.md` 来记录二改规则。不要删除或改写旧私有仓库历史说明、
Private GHCR 条件说明、历史升级记录和测试夹具；只更新明确表示“当前状态”或“当前
基线”的内容。

## 验证要求

按修改范围执行最小但充分的验证，并优先复用 `Makefile`、`deploy/tests/` 和 CI 中的
现有入口：

- 所有修改至少运行 `git diff --check`。
- 差异文件变化后，针对包含未提交修改的候选树运行
  `deploy/tests/custom-upstream-delta-test.sh`。
- 影子映射或官方薄接入点变化时运行 `deploy/tests/upstream-shadow-map-test.sh`。
- 发布、工作流或容器边界变化时运行对应的 `custom-release-safety-test.sh`、
  `rehearsal-compose-safety-test.sh`、`upstream-sync-safety-test.sh` 和入口脚本测试。
- 后端修改运行相关 Go 测试；影响面较大或用户要求时运行后端全量测试和生产构建。
- 前端修改运行相关 Vitest、typecheck；影响面较大或用户要求时运行现有前端全量
  检查和生产构建。
- 生成文件修改必须由官方生成命令产生，并验证重新生成后无差异。

不得笼统声称“全部通过”。最终报告应列出实际命令、结果、未执行项及具体原因。
CRLF 警告、缺少 Docker/WSL、平台二进制格式或缺少工具等限制应如实区分为失败、
延后到 Linux CI 或未执行。

## 审查输出

普通任务只报告与当前范围相关的修改、测试和剩余风险。完整上游对比或提交前审查
还必须报告：

- 当前分支、Vendor 基线和远程关系；
- 所有相对官方不一致文件的分类；
- 整文件恢复项和 Blob 一致性；
- 保留的官方薄接入点及原因；
- 二改业务、构建、发布和运维必需项；
- 发现并修复的 P0/P1/P2 问题；
- 数据库结构是否变化；
- 已执行与未执行测试、官方继承问题和远程 Ruleset 等限制；
- 是否达到可提交、可推送或可发布条件；三者不得混为同一结论。
