# API 密钥多分组优先级与会话粘性

本文说明 API 密钥多分组路由的开关、数据结构、运行时语义、计费边界和运维检查。
功能默认关闭；关闭时 API 密钥继续按 `api_keys.group_id` 单分组运行，不启用跨组
降级或分组会话粘性。

## 1. 全局开关

系统设置键为 `api_key_multi_group_enabled`，管理后台“系统设置 → 功能开关”可以
热更新：

- 默认值为 `false`；
- 关闭时，创建和编辑继续使用旧 `group_id` 请求，查询只按主分组筛选，鉴权和
  分组 fallback 保持旧路径；
- 关闭时发送 `group_ids` 返回 `API_KEY_MULTI_GROUP_DISABLED`，避免旧环境误写
  多分组配置；
- 打开后才显示优先级编辑器并接受 `group_ids`；
- 关闭开关不会删除已保存的备用分组。API 和运行时只暴露、使用首分组；重新打开
  后原有有序列表恢复可见。

## 2. 数据与 API

Migration `193_add_api_key_groups.sql` 新增 `api_key_groups`：

- `(api_key_id, group_id)` 为复合主键；
- `(api_key_id, priority)` 唯一，`priority` 从 `0` 开始且不得为负；
- API Key 和分组外键均为 `ON DELETE CASCADE`；
- `group_id` 有反向查询索引；
- 现有非空 `api_keys.group_id` 回填为优先级 `0`。

同一 Migration 为 `batch_image_jobs` 增加可空 `group_id` 和索引。批量生图异步结算
不能重新读取可能已重排的 API Key 首分组，因此任务提交时持久化实际路由分组，后续
Usage 和结算日志继续使用该值。

`api_key_groups` 是有序列表事实源，`api_keys.group_id` 继续同步为首项兼容镜像。
分组替换、列表压紧和兼容列更新在同一事务完成。删除分组时，从所有关联列表移除
该分组并提升下一项；修改分组平台若会造成现有列表混合平台则拒绝。

API 响应新增按优先级排序的 `group_ids` 和 `groups`。`group_id/group` 始终表示
首项，空列表时为 `null`。

- `group_ids` 出现时完整替换列表，空数组表示清空；
- 旧 `group_id` 出现时替换为单元素列表，显式 `null` 表示清空；
- 两个字段同时出现返回 `400`；
- 最多 10 项，必须为无重复正整数且属于同一平台；
- 新增项检查分组状态、用户权限、订阅和最低余额；
- 纯重排或移除不重复执行新增资格检查；
- 开关打开时，`group_id` 查询匹配列表任意位置；`group_id=0` 匹配空列表。

## 3. 请求路由

一次请求分为三层：

1. API Key、用户、模型/IP、用户并发和 API Key 自身限额等分组无关检查只执行
   一次；
2. 候选分组按顺序执行只读资格检查，不占用 RPM；
3. 选到可调度账号、即将发起上游请求时，才原子占用实际分组 RPM，并以该分组
   重建 composite 目标、订阅、倍率和 handler 上下文。Composite 候选解析到当前
   协议适配器无法安全编码的目标平台时，该候选在只读阶段视为不可用并继续扫描，
   不会把请求发送到错误的平台协议。

无会话绑定时从首项开始。有有效绑定时先使用绑定分组，即使更高优先级已恢复也不
迁移。当前绑定分组不可用时，原子清除旧绑定并从列表顶部重新扫描；同一请求内每个
分组最多尝试一次。

每组先耗尽现有账号级重试。网络错误、`429`、容量不足、`5xx` 和明确的账号/分组
可用性错误可以跨组；参数错误、内容策略错误、客户端取消和其他非重试型 `4xx`
不跨组。SSE、流式响应或 WebSocket 一旦向客户端提交上游数据，不再透明切组。

多分组请求禁用分组配置中的隐式 fallback，候选只能按密钥列表顺序到达。单分组
密钥不设置该标记，完整保留旧 fallback。

## 4. 会话粘性

Redis 键由 API Key ID、协议和会话标识哈希组成，只保存实际 `group_id`，不保存
原始 API Key 或原始会话标识。TTL 为滑动 1 小时。更新和清除使用比较并设置/删除，
避免并发请求覆盖另一请求刚写入的绑定。

- 成功响应或非分组故障的客户端错误刷新当前绑定；
- 因分组故障降级成功后绑定到新分组；
- 配置重排不迁移仍在列表中的绑定；
- 分组删除、失权或再次不可用时清除绑定；
- Redis 不可用时 fail-open，本次请求仍按优先级降级，但后续请求不保证粘性。

持久化粘性的入口包括 Anthropic Messages、OpenAI Chat Completions、OpenAI
Responses HTTP/WS 和 Gemini `generateContent/streamGenerateContent`。OpenAI
优先使用会话 Header、`prompt_cache_key` 和稳定内容种子；Anthropic/Gemini
缺少显式信号时复用摘要链最长前缀匹配。无法得到稳定信号或摘要匹配时按无会话
请求处理。

Responses 另存 `previous_response_id → group_id` 的哈希映射。跨组时仅在请求
自身上下文足以安全重建时移除 `previous_response_id`；无法重建的函数调用续链
返回协议错误，不把分组专属 ID 发送到其他分组。

其他网关入口不保存跨请求粘性，但仍支持单次请求内按同一规则跨组降级。

## 5. RPM、计费与记录归属

- 用户全局 RPM 每个客户端请求最多占用一次，在首个真实分组尝试前执行；
- `(user, group)` RPM 只在该分组真实准备发起上游请求时占用一次；
- 扫描备用分组不递增 RPM；
- 原子占用时竞争超限，将该组视为当前不可用并继续下一候选；
- Redis RPM 故障沿用 fail-open；
- 单分组密钥继续使用原计费/RPM 路径。

Usage、账单扣减、倍率、订阅额度、渠道能力、Ops 选中分组和审计日志都读取当前
实际分组。候选探测和未产生可计费 Usage 的失败尝试不重复计费。

## 6. 日志、指标与排障

结构化日志包含 API Key ID、协议、实际分组、降级深度、粘性命中/失效、候选耗尽
和 Redis fail-open；不会记录密钥或原始会话标识。进程内指标快照记录粘性命中、
失效、真实尝试、跨组、耗尽、Redis fail-open 和累计降级深度。

排障顺序：

1. 确认 `api_key_multi_group_enabled` 当前值；
2. 检查 `api_key_groups` 是否按 `priority` 连续排序，首项是否与
   `api_keys.group_id` 一致；
3. 检查候选分组状态、用户权限、订阅、最新最低余额、平台额度和 RPM；
4. 查询 `gateway.group_route_candidate`、`gateway.group_route_attempt`、
   `gateway.group_sticky_*` 和 `gateway.group_route_exhausted`；
5. Redis 故障时确认请求仍成功 fail-open，并预期会话可能回到首个可用分组；
6. Responses 续链错误时检查请求是否携带可独立重建的 input，不复制或记录完整
   请求体和 `previous_response_id`。

## 7. 部署与回退

Migration 193 为单向结构升级。部署前备份 `api_keys`、`groups` 和
`batch_image_jobs`，人工审核 SQL、Ent Schema、两个级联外键、唯一约束、回填语句
及批量生图实际分组写入，再执行项目数据库边界门禁。应用升级后默认开关仍关闭，可先
验证单分组兼容路径，再由管理员显式开启。

二进制回退前应先关闭开关。旧二进制会忽略 `api_key_groups` 并继续读取
`api_keys.group_id`；关联表可保留供再次升级使用。删除表或回滚结构不属于自动
回退步骤，必须另行人工评审。
