# Sub2API 同机隔离演练

本目录用于在已经运行官方 Sub2API 的服务器上，额外启动一套独立、低负载的
自定义版本演练环境。它不会复用生产 PostgreSQL、Redis、数据目录、runtime、
容器、网络或端口。默认只监听服务器回环地址；确有长期测试需要时，可以通过
显式双开关开放服务器公网 IP，并配置独立域名和 HTTPS。

演练环境只适合功能验证，不允许进行压力测试。Compose 对三个容器设置的资源
上限合计为 1.15 GiB 内存和 1.25 核 CPU，且全部使用
`restart: unless-stopped`。网页重启、进程异常退出或 Docker 服务重启后会
自动拉起；执行 `docker compose stop` 后则保持停止，直到管理员再次启动。

## 1. 安全边界

- 必须部署到 `/www/wwwroot/sub2api-rehearsal` 等独立目录。
- 禁止放入 `/www/wwwroot/sub2api-deploy` 或其子目录。
- Web 端口默认绑定 `127.0.0.1`；公网模式必须同时设置公开绑定确认开关。
- 数据库必须使用本 Compose 创建的全新 PostgreSQL。
- 不导入生产用户、API Key、账号凭据或真实邮件配置。
- 不挂载 Docker Socket。
- 不使用 `latest` 镜像。
- 禁止执行 `docker compose down -v`，除非已经明确决定永久删除演练数据。

## 2. 上传文件

把本目录中的文件传到服务器：

```text
/www/wwwroot/sub2api-rehearsal/
├── docker-compose.yml
├── .env.example
└── preflight.sh
```

初始化目录：

```bash
mkdir -p /www/wwwroot/sub2api-rehearsal
cd /www/wwwroot/sub2api-rehearsal
mkdir -p data runtime
cp .env.example .env
chmod 600 .env
```

如果默认端口 `18081` 也被占用，只修改 `.env` 中的
`REHEARSAL_PORT`。先检查候选端口：

```bash
ss -lntp | grep ':18081 ' || echo '18081 is free'
```

## 3. 配置演练凭据

生成独立测试密码，不要复用生产密码：

```bash
openssl rand -hex 24
openssl rand -hex 24
openssl rand -hex 24
openssl rand -hex 32
openssl rand -hex 32
```

依次填写 `.env` 中的 PostgreSQL、Redis、管理员、JWT 和 TOTP 值，确保不再
包含 `CHANGE_ME`。

公开 Release 可匿名查询和下载，演练环境默认不配置 GitHub Token。如果匿名
API 请求触发 GitHub 限流，可以在受控环境中通过 `UPDATE_GITHUB_TOKEN` 提高
限额，但不得把 Token 写入仓库、截图或日志。

GHCR 可见性与仓库可见性独立。如果镜像包设置为 Public，可以匿名拉取；如果
暂时仍是 Private，需要 Personal Access Token (classic) 且只授予
`read:packages`，该凭据只供宿主机 Docker 使用，不得挂载进应用容器：

```bash
read -rsp 'GHCR read token: ' GHCR_TOKEN
echo
printf '%s' "$GHCR_TOKEN" |
  docker login ghcr.io -u o87110 --password-stdin
unset GHCR_TOKEN
chmod 700 /root/.docker
chmod 600 /root/.docker/config.json
```

应看到 `Login Succeeded`。如果 `docker compose pull` 返回 GHCR
`unauthorized`，先核对包可见性和宿主机登录状态。

## 4. 启动前检查

在演练目录执行：

```bash
cd /www/wwwroot/sub2api-rehearsal
/bin/bash preflight.sh
```

检查通过后拉取并启动：

```bash
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --tail=200 sub2api
```

同时确认原生产容器仍正常：

```bash
docker ps --filter name='^/sub2api$'
curl --fail --silent --show-error http://127.0.0.1:8080/health
curl --fail --silent --show-error http://127.0.0.1:18081/health
docker stats --no-stream
```

如果 `.env` 选择了其他端口，将命令中的 `18081` 替换为实际端口。

首次日志应包含数据库和 Redis 连接成功、管理员创建成功以及
`Server started`。模型定价本地 fallback 文件不存在但随后成功在线下载时，
不属于启动失败。

## 5. 访问后台

### 5.1 默认方式：SSH 隧道

Windows 本地终端建立 SSH 隧道：

```powershell
ssh -L 18081:127.0.0.1:18081 root@服务器IP
```

保持 SSH 会话打开，然后访问：

```text
http://127.0.0.1:18081
```

两侧端口都要替换为 `.env` 中的实际演练端口。

### 5.2 显式公网方式

确需长期通过 `服务器IP:端口` 测试时，在 `.env` 同时设置：

```env
REHEARSAL_BIND_HOST=0.0.0.0
REHEARSAL_ALLOW_PUBLIC_BIND=true
```

两个配置缺一不可。执行：

```bash
/bin/bash preflight.sh
docker compose up -d --no-deps --force-recreate sub2api
docker compose ps
ss -lntp | grep ':18081 '
```

`docker compose ps` 应显示 `0.0.0.0:18081->8080/tcp`。如果服务器已经监听但
外部无法访问，还需要在云安全组或宿主机防火墙开放所选 TCP 端口。不要开放
PostgreSQL `5432` 或 Redis `6379`。

公网模式在配置域名和 HTTPS 前使用明文 HTTP，禁止复用生产管理员密码、用户、
API Key 或其他真实凭据。域名 HTTPS 反向代理完成后，应恢复：

```env
REHEARSAL_BIND_HOST=127.0.0.1
REHEARSAL_ALLOW_PUBLIC_BIND=false
```

随后重建应用容器并关闭公网演练端口，只由 Nginx/Caddy 通过 HTTPS 代理到
`127.0.0.1:18081`。

### 5.3 登录账号与版本显示

首次管理员账号取自 `.env`。如需在服务器核对：

```bash
grep -E '^(ADMIN_EMAIL|ADMIN_PASSWORD)=' .env
```

不要截图、记录或发送密码输出。数据库初始化完成后，单独修改 `.env` 不会
修改已创建管理员的密码，应登录后台进行修改。

登录后应验证：

- 左侧侧栏显示基础版本 `v0.1.162`；
- 更新弹窗显示完整构建版本 `v0.1.162-custom.7`；
- 点击检查更新后显示“已经是最新版本”；
- 回退列表能读取以前的有效公开 Release。

## 6. runtime 持久化验证

首次启动后：

```bash
docker compose exec --user root sub2api sh -c \
  'ls -l /app/image/sub2api /app/runtime/sub2api; readlink -f /proc/1/exe'
sha256sum runtime/sub2api
```

入口从 root 降权后，普通用户可能无权读取 `/proc/1/exe`，因此该检查明确使用
root。

记录 SHA256，然后只重建演练应用并自动比较：

```bash
before="$(sha256sum runtime/sub2api | awk '{print $1}')"
docker compose up -d --no-deps --force-recreate sub2api
sleep 5
after="$(sha256sum runtime/sub2api | awk '{print $1}')"
printf 'before=%s\nafter=%s\n' "$before" "$after"
test "$before" = "$after" && echo 'runtime persistence passed'
curl --fail --silent --show-error http://127.0.0.1:18081/health
```

重建前后 SHA256 必须一致，且 `/proc/1/exe` 应指向
`/app/runtime/sub2api`。

## 7. cyber_policy 验证

只创建演练数据：

1. 创建测试管理员、测试用户、测试上游账号和测试 API Key。
2. 创建 `audit-in-scope` 与 `audit-out-of-scope` 两个分组。
3. 风控中心的审计范围只选择 `audit-in-scope`。
4. 分别从两个分组触发可重复的测试 `cyber_policy` 响应。

范围内应保持官方处罚行为。范围外应保留
`action=cyber_policy_out_of_scope` 记录，但不通知、不累计、不封号、不写
会话屏蔽。不得使用生产上游账号、真实用户或真实邮件接收人。

完整断言见：

```text
../../docs/custom/TESTING_CN.md
```

## 8. 自定义更新与回退

使用当前正式公开 Release 时，可以验证：

- 未配置 Token 时可以匿名读取公开 Release。
- 后台显示当前已是最新版本。
- 如配置提高 API 限额的 Token，错误 Token 只影响更新查询，不影响业务请求。
- 可选 Token 不出现在日志、URL 和浏览器请求中。

完整的 `custom.4 → custom.5 → custom.4` 更新回退需要两个有效 Release。
不得通过伪造 Release、移动标签或手工替换 `runtime/sub2api`
绕过版本条件。

点击后台“立即重启”会结束容器内的主进程，因此三个服务必须保持
`restart: unless-stopped`。部署前和修改 Compose 后都应核对：

```bash
docker compose config | grep -n restart

for service in sub2api postgres redis; do
  container_id="$(docker compose ps -q "$service")"
  docker inspect "$container_id" \
    --format '{{.Name}} restart={{.HostConfig.RestartPolicy.Name}}'
done
```

更新并点击重启后，短暂的 502 属于进程切换窗口；应用容器应自动重新进入
`healthy`，页面刷新后恢复。如果旧版 Compose 使用了 `restart: "no"`，容器
会停在 Exited 状态。先执行 `docker compose start sub2api` 恢复服务，再把
Compose 和现有三个容器的重启策略改为 `unless-stopped`。

## 9. 停止演练环境

完成当天测试后停止容器，避免持续占用生产服务器资源：

```bash
cd /www/wwwroot/sub2api-rehearsal
docker compose stop
```

需要移除演练容器和网络但保留数据时：

```bash
docker compose down
```

不要添加 `-v`。保留 `data/`、`runtime/`、PostgreSQL 和 Redis 卷，便于后续
继续验证或调查问题。
