# noise-server

Noise 的独立后端，使用 Go + Gin + SQLite，为 noise-admin、noise 看板和 noise-agent 提供接口。适用于当前约 16 台节点的部署。

## 已实现

- 单管理员、bcrypt 密码哈希、持久化登录会话、登录限流、精确 CORS 与管理端写请求 Origin 校验。
- 节点、品牌、分组、账单、流量额度及安装命令。
- ICMP/TCP 监控任务、每任务间隔、服务器多选绑定、稳定的任务顺序。
- Agent 基础信息、指标上报、配置读取、延迟结果批量上报。
- 公开节点、历史指标、延迟历史、SSE 实时快照和公开站点配置。
- Telegram 渠道管理、未保存配置测试、GeoIP 三种提供商的查询测试。
- 七天历史清理、在线状态判定、到期日自动顺延、流量重置周期及重启后的计数处理。

这里提供后端实现；两个前端现有的 Mock/localStorage 尚未替换为 API，noise-agent 采集程序也不在本项目内。接口契约见 [docs/API.md](docs/API.md)。

## 目录与职责

```text
cmd/noise-server/          启动入口、进程信号
internal/app/             依赖组装、HTTP 服务、优雅关闭
internal/config/          环境变量配置
internal/httpapi/         Gin 路由、Cookie、CORS、请求/响应 DTO
internal/service/         业务规则、权限范围、公开数据投影
internal/model/           领域数据和明确的公开字段模型
internal/repository/      数据访问接口
internal/repository/sqlite/  SQL 实现、内嵌版本迁移
internal/telemetry/       线程安全的最新指标缓存
internal/scheduler/       周期性清理和到期日维护
internal/integration/     Telegram、GeoIP HTTP 客户端
deploy/                   systemd、Nginx 示例
```

请求经过 `HTTP Handler → Service → Repository → SQLite`。Handler 处理协议和身份，Service 处理业务规则，Repository 负责事务和数据持久化。新增数据库迁移放在 `internal/repository/sqlite/migrations/`，按文件名顺序执行并记录版本。

## 本地运行

开发环境使用 Go 1.27+。SQLite 驱动为纯 Go，不需要安装数据库服务或 C 编译器。

PowerShell：

```powershell
$env:NOISE_ADMIN_USERNAME = 'admin'
$env:NOISE_ADMIN_PASSWORD = '<填写你自己的 12–72 字节密码>'
$env:NOISE_COOKIE_SECURE = 'false' # 仅用于 localhost HTTP 开发
$env:NOISE_API_URL = 'http://localhost:8080'
$env:NOISE_DASHBOARD_URL = 'http://localhost:5173'
$env:NOISE_ADMIN_ORIGINS = 'http://localhost:5174'
$env:NOISE_PUBLIC_ORIGINS = 'http://localhost:5173'
go run ./cmd/noise-server
```

检查 `GET http://localhost:8080/health`。首次启动必须提供管理员密码，创建账号后密码以哈希存入 DB。后续启动可移除 `NOISE_ADMIN_PASSWORD`；重新设置这个环境变量不会覆盖已有账号。

`.env.example` 是配置清单，程序不自动读取 `.env`。可由 shell、systemd 的 `EnvironmentFile` 或部署平台注入环境变量。

## 部署到服务器，前端部署 Cloudflare

推荐域名：

```text
admin.example.com   → Cloudflare 上的 noise-admin
status.example.com  → Cloudflare 上的 noise
api.example.com     → 你的服务器上的 noise-server，通过 HTTPS 反向代理访问
```

1. 复制 `.env.example`，填写实际域名、首个管理员密码，保留 `NOISE_COOKIE_SECURE=true`。
2. 构建 Linux 二进制，例如 `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o bin/noise-server-linux-amd64 ./cmd/noise-server`。ARM64 服务器改用 `GOARCH=arm64`。
3. 将二进制放到 `/opt/noise/noise-server`；创建专用 `noise` 系统用户，将环境文件放到 `/etc/noise/noise.env`，仅允许服务用户/root 读取。
4. 在生产环境文件中设置 `NOISE_DB=/var/lib/noise/noise.db`，再使用 `deploy/noise-server.service`。其 `StateDirectory` 会为 SQLite 创建 `/var/lib/noise`。
5. 使用现有 HTTPS 网关配置 `deploy/nginx.conf`，替换域名和证书路径。SSE 必须关闭代理缓冲和缓存。

`NOISE_ADMIN_ORIGINS`、`NOISE_PUBLIC_ORIGINS` 是逗号分隔的完整 Origin，不带尾部 `/`。仅在确实使用可信代理时设置 `NOISE_TRUSTED_PROXIES`。默认不信任客户端提交的转发头。

管理端浏览器请求示例：

```js
await fetch('https://api.example.com/api/v1/admin/auth/login', {
  method: 'POST',
  credentials: 'include',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ username, password }),
})
```

后续所有管理端请求也要带 `credentials: 'include'`。Cookie 仅属于 API 主机，使用 HttpOnly、Secure、SameSite=Lax；同一主域的 HTTPS 子域支持此部署方式。CORS 仅允许配置的来源，写请求额外验证 Origin。看板公开请求无需 Cookie，原生 `EventSource` 可直接订阅 SSE。

## 数据与业务约定

- 公共 JSON 使用专门的白名单结构。节点 IPv4/IPv6、私有备注、Agent 版本和 Token 不出现在公开 REST 或 SSE 中；价格、币种、计费周期和到期日公开。监控目标也不会在公开任务信息中暴露。
- 监控间隔为每任务 1–86400 整数秒，默认 60 秒，单次超时固定 5 秒。探测由 Agent 执行，server 负责配置和收集结果。
- 新任务绑定到服务器时追加到该服务器任务末尾。`PUT /nodes/:id/latency-tasks` 按 `taskIds` 数组顺序保存。RTT 永远取首个绑定任务，不取多任务的最小值/平均值。
- 首任务超时、失败、结果超过 `interval + 5` 秒、服务器离线或没有任务时，RTT 为 `null`。删除/解绑首任务后才顺延到下一个任务。
- 节点超过 `max(10, refresh × 3)` 秒没有新的指标上报即离线。重复报告不会刷新心跳。
- 自动顺延仅在到期日已过去且节点仍在线时执行，不进行实际扣费。到期当天仍有效；月末锚点持久化，1 月 31 日 → 2 月 28 日 → 3 月 31 日。
- 计费日期和流量重置按 `NOISE_TIMEZONE`，默认 Asia/Shanghai。接口时间戳为 Unix 毫秒 UTC，日期字段为 `YYYY-MM-DD`。
- 网络速率为 **B/s**，容量/流量为 **bytes**，CPU/内存/磁盘为百分比，延迟为毫秒。现有 noise 前端的速率使用 KB/s、内存使用 MiB、磁盘容量使用 GiB，接 API 时需要在 DataSource 适配器换算。
- Agent 上报的是 OS 启动以来的累计上下行计数和 **OS uptime**。server 计算相邻报告差值；首次上报作为基线，不把接入前的流量计入当月。进程重启保持差分，OS 重启/计数下降会按新计数继续累计。跨重置边界的一次差值归入收到报告时所在周期。
- 历史指标、探测结果保留七天，每分钟清理一次；查询也限制在最近七天。历史接口自动聚合到约 1000 个时间桶，避免一次加载整周的秒级数据。延迟桶保留最后一次探测及其失败状态。
- 月度流量账单汇总不属于探测历史，保留当前及近期周期；生命周期总流量、节点配置和最后在线时间不会随历史清理丢失。
- SQLite 使用 WAL、外键、忙等待和事务；原始指标成功提交后才确认上报，最近状态会在重启时恢复。约 16 台节点每秒上报，七天约 970 万条指标，可通过后台刷新间隔配置降低采样频率。

运行时主文件为 `noise.db`，WAL 模式还会创建 `noise.db-wal` 和 `noise.db-shm`。最简单的备份方式是先停止服务再复制数据库，或使用 SQLite 的在线备份机制；不要在运行时只复制主文件而忽略未合并的 WAL。

## 验证

```text
go test ./...
go vet ./...
go build ./cmd/noise-server
```

测试使用临时 SQLite 文件和模拟第三方 HTTP 响应，不会向真实 Telegram 群组发送消息。覆盖 CORS/会话/Origin、公开字段隔离、任务绑定回滚与顺序、RTT 超时、指标幂等、并发乱序、计数重启恢复、保留期限、月末顺延及 SSE。
