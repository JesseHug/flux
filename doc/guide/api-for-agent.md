# AI Agent API 接入与操作指南

FLUX 面板提供受控的长期 **API Token（`flx_...`）** 与自检接口，方便脚本与 AI Agent 安全查询状态、调整入站配置、核对实际下发内核配置，并管理订阅引用。

> 本文档严格对应后端实现与契约测试；文中列出的路由均为 `POST` JSON 接口，响应遵循统一信封格式。

## 1. 认证与最小权限安全边界

### 1.1 生成 API Token

1. 使用管理员 Web 会话在 **系统设置 -> 安全与登录** 中创建 API Token。
2. 后端仅保存 Token 的 SHA-256 哈希；明文 `flx_...` **仅在创建成功时返回一次**，无法再次查看。
3. 可选配置 **IP / CIDR 白名单**（支持逗号、空格或换行分隔，例如 `10.0.0.0/8, 192.168.1.5`）和 **过期天数**。

### 1.2 请求头传参

面板对普通用户登录继续使用原始 JWT（`Authorization: <jwt>`，无 `Bearer` 前缀）；对以 `flx_` 开头的 API Token 支持以下任一请求头：

- `X-API-Key: flx_<token>`（推荐，最不易与 JWT 混淆）
- `Authorization: flx_<token>`
- `Authorization: Bearer flx_<token>`

### 1.3 Scope 分级与硬编码禁区（无法提权）

API Token 仅开放以下两个受限 Scope，**面板刻意不提供完整管理员权限的 API Token**：

| Scope | 允许能力 | 典型用途 |
|-------|---------|----------|
| `readonly` | 节点/内核/入站/隧道/转发/订阅的只读查询、订阅预览、`POST /api/v1/kernel/rendered`（脱敏）与 `"dryRun": true` 预演 | 巡检、比对订阅与实际渲染配置 |
| `operator` | 包含 `readonly` 全部能力，加开入站增删改/局部补丁/导入/标签/中转管理、内核同步（`control` 仅限同步/重启/启停）、订阅保存/清理孤儿引用等常规运维 | 自动化调整节点、改名、绑定出站、清理订阅 |

> [!CAUTION]
> **对所有 API Token（含 `operator`）永久硬封禁（`code: 403`）的高危接口**：
> - Token 自身管理：`/api/v1/api-token/*`（必须 Web 管理员 JWT 签发与吊销）
> - 用户与密码/通行证：`/api/v1/user/*`
> - 系统设置读写：`/api/v1/config/*`
> - 数据库/配置备份与还原：`/api/v1/backup/*`
> - 面板自升级与联邦认证：`/api/v1/system/upgrade`、`/api/v1/system/check-updates`、`/api/v1/federation/*`
> - 公告与通知通道密钥：`/api/v1/announcement/*`、`/api/v1/notify/*`
> - 宿主机/SSH/内核安装级操作：`/api/v1/node/create|update|delete|batch-delete|install|upgrade|batch-upgrade|rollback|tune|nftables/*`、`/api/v1/kernel/install`，以及 `/api/v1/kernel/control` 中的 `action: "remove"`。

---

## 2. 统一响应格式与预演约定

- 所有业务请求均为 **`POST`**，请求体 `Content-Type: application/json`，空参数传 `{}`。
- 统一响应信封：
  ```json
  { "code": 0, "msg": "操作成功", "data": {}, "ts": 1732200000000 }
  ```
  - `code === 0` 表示成功；非 `0` 时查看 `msg`（注意：鉴权失败或拒绝访问时 HTTP 状态仍可能为 `200`，由信封 `code` 返回 `401`/`403` 或 `-1`）。
- 预演模式：在支持的写入接口 JSON 请求体中传入 `"dryRun": true`，会在不写入数据库、不下发节点内核的前提下校验参数并返回脱敏后的配置差异预览。

---

## 3. 节点与内核自检接口

### 3.1 列出节点与内核状态

- `POST /api/v1/node/list` `{}` — 获取节点基础信息（ID、名称、v4/v6 地址、在线状态等）。
- `POST /api/v1/kernel/list` `{}` — 获取节点上的 sing-box / xray / snell 内核实例与同步哈希。
- `POST /api/v1/kernel/logs` `{ "id": 1 }` — 查询节点内核最新运行日志与状态。

### 3.2 查看实际下发的脱敏内核配置（替代 SSH 上机排查）

- `POST /api/v1/kernel/rendered`

请求体示例（传 `id`，或同时传 `nodeId` 与 `kernelType`）：
```json
{
  "nodeId": 1,
  "kernelType": "singbox"
}
```

返回示例：
```json
{
  "code": 0,
  "data": {
    "id": 1,
    "nodeId": 1,
    "nodeName": "hk-node",
    "nodeOnline": true,
    "kernelType": "singbox",
    "configHash": "a1b2c3...",
    "appliedHash": "a1b2c3...",
    "synced": true,
    "redacted": true,
    "files": [
      {
        "name": "singbox.json",
        "content": "{ ...密钥已脱敏为 ***REDACTED***... }"
      }
    ]
  }
}
```

- 密钥保护：`password`、`private_key`、`privateKey`、`secretKey`、`decryption`、`psk`、`shadowTlsPassword`、`certificate`、`key` 等敏感字段统一替换为 `"***REDACTED***"`。
- `configHash` 由**未脱敏的真实配置**计算得出，可与 `appliedHash`（及 `synced`）直接比对是否与节点实际运行配置一致。

### 3.3 控制内核同步与启停

- `POST /api/v1/kernel/control`

```json
{ "id": 1, "action": "sync" }
```
> `id` 为 `/api/v1/kernel/list` 返回的内核实例 ID；`action` 支持 `sync` / `restart` / `start` / `stop`（`operator` Token 可用；`remove` 对所有 API Token 永久禁止）。

---

## 4. 入站管理（含防误清空与双栈导出）

### 4.1 查询入站列表

- `POST /api/v1/inbound/list` `{}`（支持可选 `"nodeId"` 过滤）

### 4.2 全量更新与安全的局部补丁（重要）

1. **`POST /api/v1/inbound/update`**（改名、改分流、改绑定出站等）
   - 未传入的字段（包括 `outboundId`、`port`、`splitRules`、`remark`、`relayOnly`、`dialerProxyInboundId` 等）**现在会保留数据库原值**，不会再静默清零。
   - **列表返回字段对称性**：可将 `/api/v1/inbound/list` 返回的对象直接改完传回，后端会忽略只读视图字段，不再因未知字段报 `请求参数错误`。

2. **`POST /api/v1/inbound/patch`**（推荐用于 Agent 自动化局部修改）
   - 仅需传递 `id` 与你要改动的一个或多个字段：
     ```json
     {
       "id": 465,
       "name": "HK-家宽-01",
       "outboundId": 12
     }
     ```
   - 预演模式（也适用于 `/api/v1/inbound/create` 与 `/api/v1/inbound/update`）：
     传入 `"dryRun": true`，会校验参数并返回 `dryRun: true`、`changed`、`beforeHash`、`afterHash`、`beforeFiles`、`afterFiles`（已脱敏）、`redacted: true`，不写库、不重推节点。

### 4.3 创建、删除、标签与单条双栈导出

- `POST /api/v1/inbound/create` — 创建托管入站（支持 `"dryRun": true` 预演）。
  - **单入站双栈一次导出**：在同一托管入站所属节点同时具备 `IPv4` 与 `IPv6` 时，设置 `"shareAddressMode": "dual"`，订阅和 `/api/v1/inbound/share` 将自动同时导出 IPv4 与 `· v6` 两条节点，无需再手动建第二条外部节点。
- `POST /api/v1/inbound/delete` `{ "id": 465 }` — 删除入站，并自动清理引用了该入站独占名称的订阅链式绑定与落点列表。
- `POST /api/v1/inbound/tags` `{ "ids": [465], "tags": ["home", "hk"] }` — 批量设置标签（已修复持久化丢标签问题）。
- `POST /api/v1/inbound/share` `{ "id": 465 }` — 获取单条入站的导出分享链接（含双栈与中转入口）。

### 4.4 外部手工节点导入（含 VLESS + Reality）

- `POST /api/v1/inbound/import`
  支持直接导入标准分享链接，包括 **`vless://...security=reality`**、Shadowsocks、Hysteria2、AnyTLS、SOCKS5 等，每行一条链接：
  ```json
  {
    "links": "vless://<uuid>@1.2.3.4:443?encryption=none&security=reality&sni=www.apple.com&fp=chrome&pbk=<pbk>&sid=<sid>&type=tcp#US-Reality",
    "relayOnly": false,
    "tags": ["external"]
  }
  ```
  > 若仅作链式落地出口而不想出现在用户直连订阅中，传 `"relayOnly": true`。

- `POST /api/v1/inbound/external/update` — 修改外部手工节点（修改 `name` 时同样自动联动同步订阅中的链式引用名称）。

### 4.5 入站中转线（Relay）

- `POST /api/v1/inbound/relay/list` `{ "inboundId": 465 }`
- `POST /api/v1/inbound/relay/create` `{ "inboundId": 465, "tunnelId": 3, "inPort": 23456, "name": "广港中转" }`
- `POST /api/v1/inbound/relay/update` `{ "id": 10, "name": "广港中转-新", "shareHost": "" }`（改名自动联动同步订阅中的链式引用名称）
- `POST /api/v1/inbound/relay/delete` `{ "id": 10 }`（删除自动清理孤儿引用）

### 4.6 Cloudflare CDN 回源与 CF 优选池

- **入站 CDN 与 PEM 证书配置**：在 `/api/v1/inbound/create`、`/api/v1/inbound/update`、`/api/v1/inbound/patch` 中：
  - `security.certMode` 支持 `"pem"`，可直接在 `security.certPem` / `security.keyPem` 传入 Cloudflare 15 年源服务器证书（Origin Certificate）或自定义证书 PEM，随内核配置内联下发。
  - `cdn` 对象支持：`{ "enabled": true, "domain": "cdn.example.com", "edgePort": 443, "edgeTls": true, "poolId": 1, "customEntries": "104.16.132.229:443#移动", "keepOriginHost": true, "onlyCfOrigin": true }`。
  - 启用 `onlyCfOrigin: true` 后，节点 `nftables` 防火墙白名单会自动并入 Cloudflare 官方回源 IPv4/IPv6 CIDR；`/api/v1/inbound/share` 与订阅会自动展开优选池多条节点（`Host = SNI = domain`，强制开启客户端边缘 TLS 证书校验）。
- **CF 优选池接口**：
  - `POST /api/v1/cf-pool/list` `{}` — 查询优选池列表（`readonly` / `operator` 均可用）。
  - `POST /api/v1/cf-pool/parse-preview` `{ "rawText": "...", "defaultPort": 443, "maxEntries": 10, "ipFilter": "all" }` — 预览解析结果（`readonly` / `operator` 均可用）。
  - `POST /api/v1/cf-pool/save` — 创建或更新手动列表 / 远程 URL 定时拉取优选池（`operator` 可用）。
  - `POST /api/v1/cf-pool/refresh` `{ "id": 1 }` — 立即从远程 URL 拉取或重解析优选池（拉取失败自动保留旧缓存，`operator` 可用）。
  - `POST /api/v1/cf-pool/delete` `{ "id": 1, "force": false }` — 删除优选池（`force: true` 时自动解绑引用的入站，`operator` 可用）。

---

## 5. 订阅与孤儿引用清理

### 5.1 查询与保存（完整字段对称）

- `POST /api/v1/subscription/list` `{}` — 返回完整订阅列表（含只读字段 `sharedIds`、`token`、`path`、`nodeCount`、`active` 等）。
- `POST /api/v1/subscription/save` — 支持将 `/api/v1/subscription/list` 返回的对象直接修改后保存：
  - `sharedIds`、`token`、`path`、`active`、`nodeCount` 等只读视图字段会自动忽略，**不会再报参数错误**。
  - 未传的字段（如省略 `options`、`groupIds`、`remark`、`expireTime` 等）在编辑现有订阅时会**保留数据库原值**，不会静默清空。若需显式重置选项，传 `"options": {}`。

### 5.2 订阅预览与一键清理孤儿引用

- `POST /api/v1/subscription/preview` `{ "id": 1 }`（也支持传入临时 `groupIds`/`options` 做预演）。
- `POST /api/v1/subscription/prune-refs` `{}`（或传 `{ "dryRun": true }` 先预览）：
  自动扫描全部订阅，清理 `options.chainLandingNames`、`options.chainRelayNames`、`options.chainNoneNames`、`options.chainNodeBindings` 中已不存在的入站/中转历史命名残留，并返回 `{ "dryRun": false, "updatedSubscriptions": ..., "removed": [...] }`。

---

## 6. 转发隧道查询（只读上下文）

- `POST /api/v1/tunnel/list` `{}` — 获取可用于 `inbound/relay/create` 的转发隧道列表。
- `POST /api/v1/forward/list` `{}` — 获取面板转发规则列表。

---

## 7. 系统内置防踩坑开关说明（仅 Web 管理员配置）

- **`singbox_ntp_mode`**（默认 `auto`）：
  - `auto`（推荐）：检测到容器环境（无修改系统时钟权限）时，自动为 `sing-box` 注入用户态 `ntp` 模块（`time.apple.com:123`），解决系统时钟偏差导致 **SS-2022** 因 `±30s` 防重放窗口全部超时的故障。
  - `always` / `never`：强制全开启或全关闭。
- **`singbox_cache_file`**（默认开启 `true`）：
  - 仅在配置包含 `.srs` 分流规则集时生成 `experimental.cache_file`（`cache.db`，实测约 64–128KB，不会撑爆小内存 NAT 机，可加速规则集重启加载并在 GitHub 抖动时保留缓存）。如极简只读磁盘环境需要，可在 Web 设置关闭。
