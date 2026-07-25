# 二改功能清单

本文档描述相对官方基线 `vendor-0.1.164` 的有效二改需求。文件级实现范围以
`.github/custom-upstream-delta.tsv` 为准。

## 1. cyber_policy 审计范围隔离

### 目标

官方 `cyber_policy` 处罚仅对管理员配置的审计分组生效。范围外请求仍保留必要
的安全审计记录，但不得通知、累计违规次数、封号或写入会话屏蔽。

### 行为约束

- 范围内保持官方处罚语义。
- 范围外记录 `action=cyber_policy_out_of_scope`。
- 范围变化必须在下一次请求生效。
- 关键词命中摘要必须脱敏，不记录 Token、密钥、完整 URL 或完整请求体。
- 管理页面必须明确显示审计范围和当前状态。

### 主要实现

```text
backend/internal/custom/moderation/
backend/internal/service/custom_moderation_bridge.go
frontend/src/custom/moderation/
```

## 2. 固定公开 Release 更新源

### 目标

在线更新、版本查询和回退固定使用：

```text
o87110/sub2api-custom-public
```

调用方不能通过环境变量、请求参数或缓存内容切换仓库。官方仓库只用于显示官方
版本信息，不能作为自定义构建的安装回退源。

### 公开访问

公开 Release 元数据和资产默认匿名读取。`UPDATE_GITHUB_TOKEN` 与
`UPDATE_GITHUB_TOKEN_FILE` 仅用于提高 GitHub API 限额或兼容受控部署；未配置
Token 不应导致公开更新失败。

配置 Token 时仍必须满足：

- 只发送到精确 HTTPS `api.github.com`；
- 重定向到资产存储主机前删除 Authorization；
- Token 文件优先于环境变量；
- Token 文件配置错误时明确失败，不能静默改用环境变量；
- 日志、错误和浏览器响应不得出现 Token。

### 下载约束

- 资产 URL 必须属于固定仓库和受信任 GitHub 主机。
- 标签必须符合 `vX.Y.Z-custom.N`。
- 二进制与 `checksums.txt` 必须同时存在。
- 下载大小受限并校验 SHA256。
- 解压目标必须防止路径穿越和符号链接攻击。
- 新二进制必须通过 `--version` 验证后才允许替换。

## 3. 基础版本与构建版本分离

版本面板同时展示：

- 基础版本：`X.Y.Z`，用于与官方正式版本比较；
- 构建版本：`X.Y.Z-custom.N`，用于自定义更新和回退；
- 官方最新版本及独立查询警告。

自定义版本比较必须使用完整构建版本。官方查询失败不能覆盖已经取得的自定义
Release 信息；自定义源失败也不能回退安装官方资产。

缓存必须包含仓库标识和完整构建版本，拒绝跨仓库、旧格式或受污染缓存。

## 4. 在线更新二进制持久化

容器镜像中的 `/app/image/sub2api` 是只读基线，运行二进制保存在：

```text
/app/runtime/sub2api
```

首次启动从镜像初始化 runtime；local Compose 使用宿主机 `./runtime`，
standalone Compose 使用 `sub2api_runtime` 命名卷，容器重建继续使用已验证的
runtime 文件。更新使用原子替换，保留一个可回退备份，失败时恢复原可执行文件。
入口脚本必须拒绝 runtime、镜像、旧镜像和备份路径（包括悬空链接）本身为符号
链接，并在版本探测、复制、替换和执行前再次检查。

## 5. 可验证 Release 与 GHCR

发布标签格式为 `vX.Y.Z-custom.N`。正式发布必须：

- 绑定准确 Tag、提交和已通过的 CI；
- 一次构建生成 Linux `amd64`、`arm64` 归档及 OCI Layout；
- 生成 `checksums.txt` 和不可变 Release Manifest；
- 校验 Artifact ID、Digest、文件清单和 OCI Manifest；
- 使用完整版本 GHCR 标签，不发布可漂移的 `latest`；
- 不覆盖已发布资产，不移动已有 Tag；
- Release 和 GHCR 任一不完整时视为不可部署。

公开仓库的 GitHub Release 与当前 GHCR Package 均支持匿名访问。GHCR 可见性独立
于仓库可见性，发布后必须确认 Package 仍为 Public；只有未来明确改为 Private 时
才启用文档中的条件拉取鉴权。

## 6. 官方升级隔离

- `main` 保存二改主线。
- `upstream/main` 表示官方远程尚未审核的最新主线，不作为当前基线。
- `origin/upstream/main` 保存本仓库已经审核的官方镜像提交。
- `vendor-X.Y.Z` 标记已审查并合入的官方基线。
- `upgrade/vX.Y.Z` 保存可复现的升级现场。

升级必须检查冲突、二改保护路径、影子来源、数据库语义、后端测试、前端测试和
Release 预构建。失败分支不得重置或强推，以便继续修复和审查。

## 7. 数据库与差异门禁

`.github/custom-upstream-delta.tsv` 固定当前二改文件集合、Blob 和验证方法；
`.github/custom-database-exceptions.tsv` 只允许经过说明的数据库边界例外。

普通变更和官方升级都必须阻止：

- 未登记的 Migration 或 Schema 变化；
- 保护路径被官方升级静默覆盖；
- 影子来源重新进入运行时而绕过 Custom 实现；
- 生成代码未更新；
- 台账与候选 Tree 不一致。

## 8. API 密钥当前页批量操作

用户 API 密钥列表支持勾选当前页密钥，并执行以下操作：

- 批量切换到一个当前可用分组，不支持批量解除分组；
- 批量禁用，其中已经禁用的密钥直接跳过；
- 批量删除，执行前必须进行危险操作二次确认。

批量禁用同样需要二次确认。批量请求复用单密钥更新和删除接口，最多同时执行
5 个请求；不提供事务回滚。成功或已经满足目标的密钥从选择中移除，失败项保留
选中供重试，前端只显示成功、跳过和失败数量，不暴露密钥值或后端敏感错误。

选择范围固定为当前页。分页、页大小、筛选、排序或手动刷新会清空选择；不支持
跨页累计选择或直接选择全部筛选结果。删除当前非首页的全部行后回退到上一页。

主要实现：

```text
frontend/src/custom/api-keys/
frontend/src/views/user/KeysView.vue
```

## 9. 支付宝/微信多渠道用户选择

管理员可以同时启用易支付与官方支付宝/微信实例。充值和订阅页按服务商聚合并
展示以下稳定渠道：`easypay_alipay`、`official_alipay`、`easypay_wxpay`、
`official_wxpay`；同一渠道的多个实例继续由现有负载均衡策略分配。

- 默认顺序固定为易支付支付宝、官方支付宝、易支付微信、官方微信，再到 Stripe、
  Airwallex 和自定义方式；
- 新前端通过 `payment_type + provider_key` 精确选源，旧客户端不传
  `provider_key` 时保留后台默认来源逻辑；
- 币种、费率、单笔限额和每日限额只在同一渠道内聚合，单笔限额按本金加手续费后的
  网关实付金额判断；同渠道多实例存在不连续单笔区间时通过增量
  `amount_ranges` 保留各安全区间，旧客户端只看到首个连续区间，混合币种只隐藏
  受影响渠道；
- 显式渠道的全部实例均超过限额时失败关闭，不回退到超限实例；旧客户端不传
  `provider_key` 时继续保留原路由兼容行为；
- 实例选定后保存运行配置修订指纹，下单前及调用网关前重新确认实例仍启用且配置
  未变化；金额限额复核无法查询当前用量时失败关闭；
- 前端金额、手续费与订阅汇率换算按渠道币种最小单位进行精确十进制舍入，零小数
  币种拒绝小数金额，BHD、KWD、JOD 等三位小数币种允许三位小数输入；
- 网关失败只提示用户手动切换可见备用渠道，不自动跨渠道创建第二个订单；
- 移动端同一渠道转二维码、微信 OAuth/JSAPI 和恢复令牌保持原 `provider_key`；
- 旧后端没有 `method_options`、旧恢复快照或旧微信令牌没有渠道字段时继续兼容；
- 本功能不新增 Migration、Schema、实体字段或 SQL。

主要实现：

```text
backend/internal/custom/paymentchannels/
frontend/src/custom/payment-channels/
```

## 10. 明确没有改变的行为

除本文档列出的边界外，认证、计费、调度、协议兼容和官方功能应保持当前
源码既有行为。新增需求必须先更新本清单和对应测试，不能用“二改”名义扩大隐式
行为范围。
