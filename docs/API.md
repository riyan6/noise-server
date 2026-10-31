# Noise HTTP API v1

Base URL 示例：`https://api.example.com`。除 SSE 外，成功响应均使用 HTTP 200：

```json
{"status":"success","data":{}}
```

失败响应使用对应 HTTP 状态码：

```json
{"status":"error","code":"invalid_request","message":"..."}
```

`400 invalid_request/invalid_json`、`401 unauthorized`、`403 origin_required/origin_not_allowed`、`404 not_found`、`409 conflict`、`413 body_too_large`、`429 rate_limited`、`500 internal_error`。SSE 连接上限返回 `503 stream_limit`。请求体最大 1 MiB，未知 JSON 字段会被拒绝。空集合为 `[]`，未知/不可用的读数为 `null`。

ID 是不透明字符串，不使用节点名称作为 ID。`createdAt/updatedAt/time/lastSeen/from/to/receivedAt` 为 Unix **毫秒**。`interval/timeout/refresh/step` 为**秒**，延迟读数为**毫秒**，网络速率为 **B/s**，容量和流量为 **bytes**。JSON 字段采用 camelCase。

## 1. 管理端

前缀：`/api/v1/admin`。

除登录外都需要 `noise_session` Cookie；浏览器 fetch 设置 `credentials: 'include'`。登录和其他写请求必须带配置允许的 Origin，浏览器会自动添加。命令行测试也应显式传入该 Origin。Cookie 为 API 主机独有、HttpOnly，生产环境 Secure、SameSite=Lax，有效期七天。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/login` | 登录，设置 Cookie，返回当前用户 |
| POST | `/auth/logout` | 撤销当前会话并清除 Cookie |
| GET | `/auth/me` | 当前用户 `{name, role:"owner"}`；系统只有一个管理员 |
| GET | `/nodes` | 全部节点；可传 `q`、`brand`、`group` 查询参数 |
| POST | `/nodes` | 新增 `{name, brand}` |
| GET | `/nodes/:id` | 节点详情 |
| PATCH | `/nodes/:id` | 部分更新 `name/brand/group/note` |
| DELETE | `/nodes/:id` | 撤销该节点 Token，删除节点、任务关联及节点数据 |
| GET | `/brands`、`/groups` | 候选字符串数组，包括此前保存但暂时未使用的值 |
| PUT | `/nodes/:id/billing` | 替换完整账单对象 |
| PUT | `/nodes/:id/traffic` | 替换完整流量配置 |
| GET | `/nodes/:id/install` | `{nodeId, token, command}`；需先配置安装脚本地址 |
| GET | `/latency/tasks` | 全部监控点及绑定服务器 |
| POST | `/latency/tasks` | 新增监控点 |
| PATCH | `/latency/tasks/:id` | 部分更新监控点；提交 nodeIds 时替换该任务绑定 |
| DELETE | `/latency/tasks/:id` | 删除监控点、全部关联和其探测记录 |
| PUT | `/nodes/:id/latency-tasks` | `{taskIds:[...]}`；数组顺序就是服务器任务顺序；`[]` 解除全部绑定 |
| GET | `/channels` | 已脱敏的渠道数组 |
| POST | `/channels` | 新增渠道 |
| PATCH | `/channels/:id` | 更新渠道 |
| DELETE | `/channels/:id` | 删除渠道 |
| POST | `/channels/test` | 测试当前表单配置，支持未保存的渠道 |
| POST | `/channels/:id/test` | 测试已保存的渠道，无需请求体 |
| GET | `/settings` | 完整设置 |
| PUT | `/settings` | 替换完整设置 |
| POST | `/geoip/lookup` | 查询指定 IP；可使用表单当前选择的提供商 |

### 登录

```json
{"username":"admin","password":"your-password"}
```

每个来源 IP 每十五分钟最多十次连续登录尝试，成功后重置计数。没有开放注册入口。首次账号来自服务启动环境配置。

### 节点

节点详情/列表数据示例（没有接入 Agent 时 `agent` 和 `lastSeen` 为 null）：

```json
{
  "id":"NODE_ID",
  "name":"hk-edge-01",
  "brand":"Vultr",
  "group":"edge",
  "note":"私有备注",
  "billing":{"cycle":"monthly","price":6,"currency":"USD","expiresAt":"2026-11-30","autoRenew":true},
  "traffic":{"method":"sum","bytes":2199023255552,"unit":"TB","resetDay":1},
  "agent":{"ip":"203.0.113.10","version":"0.1.0","os":"Debian 12","kernel":"6.1.0","arch":"x86_64","cpuModel":"Example CPU","cores":2,"memory":4294967296,"storage":85899345920,"region":"HK","city":"Hong Kong"},
  "createdAt":1793404800000,
  "online":true,
  "lastSeen":1793404800000,
  "latencyTaskIds":["TASK_ID"]
}
```

普通节点响应不包含原始 Token，仅安装接口返回。创建节点只需名称和品牌；IP、版本、硬件由 Agent 上报，管理端不能通过节点 PATCH 伪造这些字段。名称/品牌/分组最多 240 UTF-8 字节，私有备注最多 4096 字节。

账单对象：`cycle` 可为 `monthly/quarterly/semiannual/annual/biennial/triennial`，`currency` 可为 `CNY/USD/EUR/HKD/JPY`。金额和日期可以为 null，开启 autoRenew 必须有日期。到期日当日有效；已过期且节点在线时顺延，保留最初的月末日期锚点，不执行支付。

流量对象：`method` 可为 `sum/max/min`，`bytes:null` 表示不限，`0` 是零额度。`unit` 可为 `B/KB/MB/GB/TB/PB`，采用 1024 进制，仅用于回显输入单位；实际额度存储 bytes。`resetDay` 为 1–31，短月使用月末。空值不能代替完整的 billing/traffic 配置。

### 延迟任务

创建示例：

```json
{"name":"Cloudflare TCP","type":"tcp","target":"1.1.1.1:80","interval":60,"nodeIds":["NODE_ID"]}
```

返回字段：`id/name/type/target/interval/nodeIds/createdAt/updatedAt`。

`type` 为 icmp 或 tcp，省略时默认 icmp。ICMP 接受 IP/主机名，不含端口；TCP 必须有 1–65535 端口，IPv6 格式为 `[2606:4700:4700::1111]:443`。interval 为 1–86400 整数秒，创建时省略默认 60，固定 timeout 为 5 秒。nodeIds 省略时不绑定服务器。

`PATCH` 中省略 nodeIds 保留原绑定；提交 `[]` 解除这个任务的全部绑定。对单个服务器分配任务使用 `PUT /nodes/:id/latency-tasks`：

```json
{"taskIds":["FIRST_TASK_ID","SECOND_TASK_ID"]}
```

必须为不重复且存在的 ID；非法 ID 导致整个修改回滚。编辑监控点不会重排已经绑定到服务器的任务，新绑定追加到末尾。单服务器 PUT 才按提交的完整数组替换顺序。

### Telegram 渠道

创建/更新：

```json
{
  "name":"运维通知",
  "type":"telegram",
  "config":{
    "botToken":"123456:YOUR_BOT_TOKEN",
    "chatId":"-100123456789",
    "endpoint":"https://api.telegram.org/bot",
    "messageThreadId":null
  }
}
```

endpoint 必须是 HTTP(S) API base URL，并以 `/bot` 结尾。实际请求会追加 `botToken/sendMessage`。messageThreadId 为 null 或正整数。

返回的 `config.botToken` 为 `""`，`tokenHint` 提供掩码。PATCH 省略 config 时保留原配置；提交 config 时应包含完整配置，botToken 为 `""` 时保留已存 Token。读取渠道并编辑时，不应把掩码当成新 Token。

测试未保存配置：

```json
{"config":{"botToken":"123456:YOUR_BOT_TOKEN","chatId":"-100123456789","endpoint":"https://api.telegram.org/bot","messageThreadId":null}}
```

编辑已保存渠道但保留 Token 时，可传 `{"channelId":"CHANNEL_ID","config":{...完整配置，botToken为空字符串...}}`。不传 config、只传 channelId 则测试已保存配置。

测试结果位于标准包络的 data 中：`{ok:true}`，或 `{ok:false,reason:"rejected",description:"..."}`，或 `{ok:false,reason:"timeout"|"unreachable"}`。配置错误返回 HTTP 400；上游拒绝/网络问题返回 HTTP 200 的业务测试结果。server 生成测试消息，无任意消息转发接口。

### 设置

```json
{
  "siteName":"Noise",
  "defaultTheme":"light",
  "defaultLang":"zh",
  "refresh":1,
  "geoip":{"enabled":true,"provider":"ipinfo","userAgent":"Noise/1.0"},
  "agent":{"scriptUrl":"https://example.com/install.sh","endpoint":"https://api.example.com","dashboardUrl":"https://status.example.com"}
}
```

refresh 可为 1、2、5、10 秒；默认语言为 zh/en，主题为 light/dark。scriptUrl 可留空，其余地址必须为 HTTP(S)。这些设置保存到 SQLite；允许的前端域名、Cookie 策略、服务端时区属于环境配置，不通过此接口修改。

### GeoIP

```json
{"provider":"ipinfo","ip":"1.1.1.1","userAgent":"Noise/1.0"}
```

provider 为 ipinfo、ip-api、ip.sb。ip 和 userAgent 可省略。ip 为空时查询**服务端出口 IP**，不是浏览器 IP；私有/保留地址不会发送给提供商。返回 data 为 `{ok:true,result:{ip,countryCode,region,city,asn,asOrg,isp,latitude,longitude,timezone},raw,url}`，或 `{ok:false,reason,message?,raw?,url?}`；reason 为 rejected/reserved/timeout/unreachable。只有管理员接口返回原始响应和查询 IP。

## 2. Agent

前缀：`/api/v1/agent`。全部接口要求：

```http
Authorization: Bearer NODE_TOKEN
Content-Type: application/json
```

节点身份完全来自 Token；请求体不携带 nodeId，不能上报到其他节点。

### POST /info

```json
{"ip":"203.0.113.10","ipv6":"2001:db8::10","version":"0.1.0","os":"Debian 12","kernel":"6.1.0","arch":"x86_64","cpuModel":"Example CPU","cores":2,"memory":4294967296,"storage":85899345920,"region":"JP","city":"Tokyo"}
```

cores/memory 必须大于零，storage 非负，IP 字段若有值则必须是有效地址。启动时及基础信息变化时发送，成功 data 为 null。基础信息不会充当指标心跳。地理位置可由 Agent 根据配置中的 GeoIP 提供商查询自身出口后上报。

### GET /config

```json
{
  "version":"1793404800000000000",
  "refresh":1,
  "traffic":{"method":"sum","bytes":null,"unit":"GB","resetDay":1},
  "timezone":"Asia/Shanghai",
  "geoip":{"enabled":true,"provider":"ipinfo","userAgent":"Noise/1.0"},
  "tasks":[{"id":"TASK_ID","name":"Cloudflare","type":"icmp","target":"1.1.1.1","interval":60,"timeout":5}]
}
```

version 为不透明字符串，仅作相等比较。Agent 启动/重连时读取配置，后续指标响应中的 configVersion 不同时重新读取；可额外每 60 秒刷新一次。任务数组按稳定绑定顺序返回。Agent 对每个任务按 interval 调度，设置五秒超时，同一个任务尚未结束时不叠加执行。

累计流量不需要让 Agent 每月清零；server 按重置周期计算差值。安装命令保留的 `--month-rotate` 参数供 Agent 自身展示/兼容使用，server 的周期汇总始终是配额统计依据。

### POST /report

```json
{"bootId":"UNIQUE_AGENT_PROCESS_INSTANCE_ID","sequence":0,"uptime":3600,"cpu":12.5,"mem":30,"disk":40,"load":0.42,"up":204800,"down":102400,"totalUp":10737418240,"totalDown":21474836480}
```

bootId 每个 Agent 进程实例生成一次，进程重启使用新值；sequence 从零开始严格递增，重试同一份报告必须使用相同 bootId/sequence。uptime 必须是**OS uptime 秒数**，不是 Agent 运行时间。totalUp/totalDown 是 OS/选定网卡的累计字节，不是本月汇总；上报的网卡集合应保持一致。

成功返回 `data:{receivedAt,configVersion}`。时间以服务端收报时间为准。相同 bootId/sequence 重试返回原 receivedAt，不重复入库、不增加流量、不刷新心跳；同进程乱序报告或重新出现的旧进程实例返回 409。成功确认前数据已提交 SQLite。

资源百分比范围 0–100；指标不得为负或非有限数；累计字节计数最大 2^53−1。第一次上报建立基线。计数下降或 OS uptime 回退会按重置后的新计数继续累计。

### POST /latency-results

```json
{"results":[{"taskId":"TASK_ID","runId":"UNIQUE_PROBE_RUN_ID","time":1793404800000,"status":"ok","value":23.5},{"taskId":"ANOTHER_TASK_ID","runId":"ANOTHER_RUN_ID","status":"timeout","value":null}]}
```

每批 1–1000 个结果。runId 对一次探测唯一，重试保留它；相同节点/任务/runId 不会重复插入。time 省略/0 时使用服务端接收时间；显式时间允许最近七天、最多未来五秒的时钟偏差，轻微未来时间会归一到接收时间。

status 为 ok/timeout/error。ok 必须有 0–5000 毫秒的 value；timeout/error 必须为 null。只有当前绑定任务可上报，批次里有未授权任务则整批回滚。server 不保存或公开可能含目标 IP 的原始探测错误文本。

## 3. 公开看板

前缀：`/api/v1/public`，匿名读取，不需要 Cookie。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/settings` | `siteName/defaultTheme/defaultLang/refresh` |
| GET | `/nodes` | 全部公开节点与最新状态 |
| GET | `/nodes/:id` | 单个公开节点 |
| GET | `/nodes/:id/history` | 指标历史及首任务 RTT |
| GET | `/nodes/:id/latency` | 按监控点分别返回延迟历史 |
| GET | `/events` | SSE，每秒完整快照 |

公开节点字段：

```text
id, name, provider, group, region, city, os, kernel, arch, cpuModel,
cores, memory, storage,
billing {cycle, price, currency, expiresAt},
traffic {method, bytes, unit, resetDay},
online, lastSeen, uptime, cpu, mem, disk, up, down, load, rtt,
total, periodStart, periodUp, periodDown, periodUsed,
tasks [{id, name, type, interval}]
```

没有 IP、Token、私有备注、Agent 版本、安装地址、任务目标等字段。离线时 CPU/MEM/DISK/NET/LOAD/RTT 均为 null，uptime 为 0，保留硬件、历史和流量累计。total 为接入后累计观察到的上下行总和；periodUp/periodDown 是当期流量，periodUsed 按 sum/max/min 计量。

RTT 只使用首个绑定任务，失效时返回 null，不回退到其他任务。实时指标的每秒节奏与延迟探测的默认六十秒节奏相互独立。

### 历史查询

两个历史接口支持 `from/to`（Unix 毫秒）及 `step`（整数秒）。默认最近一小时，限制最近七天。服务端可能增大 step，使结果约为 1000 个时间桶，响应返回实际 step。from >= to、负值或无效数字返回 400。

`GET /nodes/:id/history` 的 data：

```json
{"from":1793401200000,"to":1793404800000,"step":4,"records":[{"time":1793404800000,"cpu":12.5,"mem":30,"up":204800,"down":102400,"rtt":23.5}]}
```

指标桶使用平均值，时间取桶内最后一次上报。缺报间隙插入 null 样本，避免图表将断联期间连接成正常曲线。RTT 按**当前首个任务**的历史探测结果映射；它不是某次过去绑定关系的审计记录。

`GET /nodes/:id/latency?taskId=TASK_ID` 的 data：

```json
{"from":1793401200000,"to":1793404800000,"step":4,"series":[{"task":{"id":"TASK_ID","name":"Cloudflare","type":"icmp","interval":60},"records":[{"time":1793404800000,"status":"ok","value":23.5}]}]}
```

省略 taskId 返回该节点当前全部绑定任务；指定未绑定任务返回 404。每个延迟桶保留最后一次探测，超时/失败点保留 null 值。

### SSE

```text
event: snapshot
data: {"nodes":[...公开节点...],"updatedAt":1793404800000}

```

连接后立即发送完整快照，之后每秒发送。客户端断线重连后读取新的完整快照，无增量事件重放需求。服务端共享快照序列化结果，限制每进程最多 64 条同时打开的 SSE 连接。

```js
const events = new EventSource('https://api.example.com/api/v1/public/events')
events.addEventListener('snapshot', event => {
  const { nodes, updatedAt } = JSON.parse(event.data)
  // 在 noise 的真实 DataSource 中更新稳定快照，并完成 bytes/B/s → UI 单位转换。
})
```

## 4. 健康检查

`GET /health` 无需鉴权，返回 `data:{status:"ok",version:"0.1.0"}`。此接口仅检查服务进程的 HTTP 存活状态。
