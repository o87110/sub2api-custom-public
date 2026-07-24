# 公开部署、在线更新与回退

本文档面向公开 Release 和 `ghcr.io/o87110/sub2api-custom-public` 镜像。生产凭据、
主机地址和内部拓扑不属于仓库内容。

## 1. 发布介质

每个 `vX.Y.Z-custom.N` 应包含：

- Linux `amd64` 与 `arm64` 二进制归档；
- `checksums.txt`；
- `release-manifest.json`；
- 对应架构和多架构 GHCR 标签。

不要使用 `latest`。部署必须固定完整版本标签或 `sha256` Digest。

## 2. 容器部署

复制公开示例并填写独立凭据：

```bash
cd deploy
cp .env.example .env
chmod 600 .env
```

设置准确镜像：

```dotenv
SUB2API_IMAGE=ghcr.io/o87110/sub2api-custom-public:vX.Y.Z-custom.N
```

启动：

```bash
docker compose -f docker-compose.yml -f docker-compose.custom.yml pull
docker compose -f docker-compose.yml -f docker-compose.custom.yml up -d
docker compose ps
curl --fail http://127.0.0.1:8080/health
```

如果 GHCR 包为 Public，无需登录。如果包仍为 Private，只在宿主机 Docker
Credential Store 配置 `read:packages` 凭据，不得把 GHCR Token 传入应用容器。

## 3. Release 查询鉴权

公开 Release 默认匿名读取。`UPDATE_GITHUB_TOKEN` 和
`UPDATE_GITHUB_TOKEN_FILE` 是可选兼容能力，用于提高 API 限额，不是部署前提。

如确需配置：

- 使用最小权限、可轮换凭据；
- 优先使用文件挂载；
- 不写入 `.env` 示例、Compose、命令行历史、日志或浏览器；
- 轮换时先验证新 Token，再撤销旧 Token。

## 4. runtime 持久化

生产容器必须挂载：

```text
./runtime:/app/runtime
```

入口脚本会验证 `/app/runtime/sub2api` 是普通文件、权限正确并可执行。首次启动从
镜像基线初始化；在线更新后容器重建仍使用已验证的 runtime 文件。

验证：

```bash
docker compose exec --user root sub2api sh -c \
  'ls -l /app/image/sub2api /app/runtime/sub2api; readlink -f /proc/1/exe'
sha256sum runtime/sub2api
```

## 5. 升级前备份

至少备份：

- PostgreSQL 数据库；
- `.env` 和配置文件；
- `runtime/sub2api` 及 `.backup`；
- 当前镜像 Tag 与 Digest；
- 当前 Release Manifest 和校验和。

备份文件不得提交到仓库或上传为公开 Artifact。

## 6. 在线更新

管理员界面只能安装固定公开仓库中的合法 `custom.N` Release。服务端会再次验证
仓库、标签、资产 URL、大小、SHA256 和二进制版本，然后原子替换 runtime 文件。

更新后检查：

- `/health` 返回成功；
- 当前完整构建版本正确；
- 容器重建后 runtime SHA256 不变；
- 后端、前端和数据库日志无持续错误；
- Release 链接与实际安装版本一致。

## 7. 回退

正常回退必须选择已有、有效且校验通过的公开 Release。不要移动标签、伪造资产或
手工替换二进制绕过检查。

若新二进制安装失败，更新器应恢复原文件；若需要人工恢复，可在停服并完成备份后
使用已验证的 `.backup`，随后重新执行版本和 SHA256 检查。

数据库存在不可逆 Migration 时，二进制回退不等于数据回退，必须按对应升级评审
记录处理。

## 8. 同机演练

`deploy/rehearsal/` 提供隔离环境：独立 PostgreSQL、Redis、数据目录、runtime、
端口和资源限制。演练不得导入生产用户、账号凭据、API Key 或真实邮件配置。

## 9. 生产安全原则

- GitHub Environment 使用人工审批；
- 构建 Job 不接触生产 Secrets；
- 发布 Job 只获得必要的 `contents: write` 与 `packages: write`；
- Workflow 日志不得输出 Token、服务器地址或连接字符串；
- 公共 PR 不得直接触发生产部署；
- 所有部署固定不可变版本或 Digest。
