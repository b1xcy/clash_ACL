# API 与配置

| 方法与路径 | 行为 |
| --- | --- |
| `GET /healthz` | 返回 `ok`，检查服务存活 |
| `GET /` | 返回内嵌订阅转换表单 HTML |
| `GET /api/subscriptions?url=...` | 按请求地址读取缓存状态与节点预览；refresh=true 启动后台刷新 |
| `GET /api/rules/ai` | 返回当前内置 AI 规则清单（placeholder 与完整清单使用） |
| `GET /api/status` | 返回接口能力、配置数量及不含 URL 的来源摘要 |
| `GET /api/config` | 返回当前完整配置 |
| `PUT /api/config` | 用 JSON 请求体整体替换并保存配置 |
| `GET /api/sources` | 返回可编辑的来源设置，包含订阅 URL |
| `PUT /api/sources` | 用 JSON 来源数组替换来源列表，保留其他配置 |
| `GET /api/options` | 返回重命名、AI、代理组设置及自建节点名称 |
| `PUT /api/options` | 保存转换设置，保留来源、自建节点和基础配置 |
| `GET /api/self-hosted` | 返回用于编辑的节点 JSON 文本，保留数值精度 |
| `PUT /api/self-hosted` | 替换自建节点数组，保留其他设置，拒绝删除已绑定 AI 的节点 |
| `POST /api/sources/refresh` | 启动启用来源的后台刷新，返回 HTTP 202 |
| `GET /api/sources/{id}/preview` | 从最近成功缓存返回节点名称与协议类型 |
| `POST /api/yaml/preview` | 解析请求体中的 YAML，仅预览，不保存 |
| `POST /api/uri/preview` | 解析多行协议链接，返回原行号、名称、协议、ID 与脱敏错误，不保存 |
| `POST /api/aggregate/preview` | 合并请求中的 YAML 缓存与有效直接链接，按连接参数去重并预览 |
| `GET /api/rules/presets` | 返回内置规则方案名称与 ID |
| `GET /api/rules/status` | 返回规则版本、来源、更新状态、上次检查及下次检查时间 |
| `POST /api/rules/update` | 立即后台检查/校验上游规则，返回 HTTP 202，同步任务自动合并 |
| `POST /api/rules/preview` | 读取本地 INI/规则文件，输出自定义优先的完整规则顺序 |
| `POST /api/generate/preview` | 生成实际组与完整配置，返回脱敏节点、代理组、规则数及来源状态；不返回节点凭据 |
| `POST /api/generate` | 接收转换参数并保存完整快照，返回固定下载地址和脱敏预览 |
| `GET /download?id=...` | 返回已保存 YAML；refresh=true 等待刷新和重新生成 |
| `GET /download?url=...` | 使用保存的默认设置与可选 preset 生成/读取快照 |

所有接口均无需认证。订阅地址通过重复的 url 查询参数传入，地址中的 &、? 等字符由调用端 URL 编码；aggregate=false 只读取第一条，默认启用全部。读取不会修改服务器上保存的来源列表，缓存仍保留在数据目录。

```bash
curl --get http://127.0.0.1:8080/api/subscriptions \
  --data-urlencode 'url=https://example.com/sub?token=example&format=clash' \
  --data-urlencode 'url=https://example.net/clash.yaml' \
  --data-urlencode 'aggregate=true' \
  --data-urlencode 'refresh=true'
```

接口先返回来源状态，使用相同 url 参数且省略 refresh 轮询，读取成功后响应的 previews 提供节点名称与类型。参数错误返回 400。/download 沿用同一组地址参数，也可使用生成接口返回的快照 ID。
配置读写接口保留，用于可选的服务器默认设置；前端读取订阅不依赖先保存配置。JSON 响应不缓存，日志和校验错误不打印完整来源地址或节点秘密。

配置结构包含 `fetch_concurrency`（默认 2）、`sources`、`self_hosted_nodes`、`rename_rules`、`groups`、`rules`、`ai` 与 `base`。自建节点使用 `{ "id": "fixed", "proxy": { ... } }`，`proxy` 保存完整 Mihomo 节点字段；`ai.node_id` 可引用保存的自建 ID，或 URI/订阅预览返回的稳定 node-... ID。生成器执行这些策略，base 中的客户端设置保留，mode 固定为 rule，proxies/proxy-groups/rules 由生成器统一输出。

## 生成与快照下载

POST /api/generate 接收 urls、aggregate、direct_links、policy、ai、groups（或 groups_json 文本）与 rules。省略 policy/ai/groups 使用保存的默认设置；rules 包含 preset、custom_rules、policy_map、fallback、skip_unsupported。默认方案为 acl4ssr-online；设置 preset=none 可只使用自定义规则。POST /api/generate?refresh=true 强制刷新来源。

`policy.drop_plaintext` 默认 false；开启时过滤无 TLS 的 HTTP/SOCKS5、SOCKS4、无 TLS 的 VLESS、无 TLS 且 cipher 为 none/zero 的 VMess，以及 cipher 为 none/dummy 且无加密插件的 SS（obfs TLS 伪装仍视为明文；v2ray-plugin/gost-plugin 启用 TLS 则保留，其他插件不推断）。HTTP/SOCKS5 的 TLS 字段遵循 [Mihomo 配置](https://wiki.metacubex.one/config/proxies/http/)。PUT/GET /api/options 的 `drop_plaintext` 保存／读取默认设置。聚合与生成 preview 的 `filtered_count` 为去重后丢弃数量；原订阅缓存保持完整。生成请求显式 policy 优先于保存设置，快照固定此开关，后续 refresh 沿用。全部过滤时拒绝生成；选中的 AI 出口被过滤时按缺失出口报错。

成功返回 download_path（/download?id=...）、generated_at、using_cache、refresh_succeeded 和脱敏 preview，不返回节点认证参数。完整 YAML 通过下载地址获取。直接节点、自建节点及转换参数保存于私有数据目录的 snapshots/ 文件，重启后地址仍有效；快照地址固定当时的设置，修改默认设置后须重新生成新地址。

普通下载与 refresh=false 返回已有快照；规则版本变化时使用快照内保存的完整节点参数重建分组/规则，不额外刷新机场来源，不重复执行重命名。首次没有快照时拉取缺少的来源缓存并生成。refresh=true 等待启用来源刷新与生成，沿用默认并发 2 和同来源请求合并；同一快照的并发生成也合并。失败来源有成功缓存时可继续生成，响应头 X-Clash-ACL-Using-Cache 为 true，X-Clash-ACL-Refresh-Succeeded 为 false。缺少可用来源、AI/分组/规则错误或保存失败会返回错误，保留已有完整快照。

PUT 是整体替换操作，更新前应先读取完整配置。非法 JSON、重复来源 ID、无效来源 URL 和不存在的 AI 节点引用会返回 HTTP 400，并保留原文件。成功写入使用同目录临时文件与重命名；节点扩展字段和 JSON 数字精度在读写中保留。

## 来源读取与缓存

在「订阅链接」中每行输入一个地址，点击「读取订阅」。聚合关闭时只读取第一行；展开本次来源后可查看节点和缓存。刷新任务在后台执行，页面自动更新来源状态；停用来源不会发起请求。`/api/subscriptions` 返回本次来源的节点数、上次刷新时间、错误、缓存状态与节点预览。

支持含 `proxies` 的 Clash/Mihomo 配置及 proxy-provider YAML。只提取节点，保留嵌套协议参数与扩展字段；来源自带分组和规则不合并。输入缺少节点、节点缺少名称或类型、多文档 YAML 等情况会明确报错。

- 来源缓存位于数据目录的 `cache/`，使用来源 ID 与 URL 的摘要作为文件名。修改 URL 后不会复用旧 URL 的缓存。
- 参数读取接口的并发上限为 2；已保存来源的刷新可通过 `fetch_concurrency` 调整。同一来源的重叠刷新共用一个任务。
- HTTP 请求超时为 20 秒，单个订阅上限为 8 MiB。
- 刷新失败时保留最近成功节点，显示 `using_cache: true` 和错误。没有成功缓存时明确显示失败。
- 页面与预览 API 只显示节点名称和协议，不公开服务器、密码或认证参数。完整节点参数保存在私有缓存中，供后续生成器使用。
- 「YAML 预览」支持粘贴本地配置，解析结果不会自动保存或导入。多行协议 URI 通过独立入口解析，见下文。

订阅来源可手动刷新，也可通过生成/下载请求的 refresh=true 刷新。来源订阅不进行定时后台拉取。


## 生成约束

- 协议链接覆盖 SS/TUIC/VLESS/VMess/Trojan/Hysteria2（hy2）/AnyTLS；保留认证、TLS、Reality、传输和扩展字段，节点 ID 不依赖名称。
- policy.self_hosted_ids 只接受当前直接链接稳定 ID，倍率固定 0、rate_group=self，优先于名称与倍率覆盖；未知 ID 拒绝。
- 自建标记随生成快照保存，页面重载清空链接和标记。未知倍率进入标准组，普通优先低倍率；高倍率不进入自动备用。
- groups_json 保持原始文本，经 Go 精确解码，保留大整数与长小数。代理组可使用 node_ids、proxies、filter、source_ids、rate_groups（self/low/standard/high）。
- ai.node_ids 按顺序指定多个出口，去重后生成手动选择组；旧 node_id 兼容读取，node_ids 非 null 时优先（空数组明确清除）。任一选中出口缺失拒绝生成。chain=true 对所有出口写入 dialer-proxy，生成 ⚡ AI 前置自动 url-test 和默认选自动／可手动覆盖的 🔗 AI 前置选择；所有选中出口与 DIRECT 从前置排除。ai.test_url 可填 HTTP(S) 地址，留空派生首个出口的 https://服务器/，不使用代理端口。
- 自定义 AI 规则优先，其他自定义规则优先于外部模板，最终只保留一个兜底；未知目标、空组、依赖循环拒绝。
- 规则更新失败保留有效库；快照按规则版本重建，不重新拉取节点，不重复重命名。refresh=true 才强制刷新来源。
- 所有接口无需认证；UI 预览脱敏，完整配置/API 与下载保留用户参数。
