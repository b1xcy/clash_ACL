# clash_ACL

自建的 Mihomo / Clash.Meta 订阅转换工具。合并 YAML 订阅与协议链接，统一整理节点、倍率、代理组和分流规则，生成可持续更新的订阅地址。

VLESS 链接中的 `sni` 参数转换为 Mihomo 的 `servername` 字段（TLS／Reality 均适用）。从旧版升级后，请重新导入链接并生成订阅，再在客户端更新；旧快照不会自动修复此字段。字段修正会改变节点 ID，原有自建标记及 AI 出口绑定需重新选择。YAML 输入保留原字段，请自行使用 `servername`。

## 功能

- **粘贴即导入**：订阅自动读取，直接链接自动解析、逐行提示错误，混合来源自动聚合去重。
- **节点整理**：可搜索节点表、倍率分布、重命名；支持 249 个国家／地区的国旗前缀，识别 SG/CN/HK/TW/JP 等缩写。
- **HTTP/SOCKS 过滤**：节点总览设置默认策略，每个订阅可选择跟随默认／丢弃／保留；按类型过滤 HTTP/SOCKS4/SOCKS5（含 TLS 变体），不影响 VLESS 等其他协议。预览显示丢弃数量，设置可保存并用于订阅刷新，默认关闭。
- **自建零倍率**：通过链接导入，逐节点勾选自建（0 倍率），独立 `🏠 自建` 选择组；电报组提供该选项。
- **AI 固定出口**：多选支持协议的导入／订阅节点，内置 ChatGPT/Codex、Claude、Gemini、Copilot 等规则，无需在额外域名框重复填写。
- **链式代理**：为全部 AI 出口生成 url-test 自动优选和手动前置组；最终出口只在勾选节点中切换。
- **规则更新**：17 个 ACL4SSR 官网命名方案，默认每 24 小时检查上游，也可手动更新；失败保留有效库。
- **配置导出**：预览实际组，持久化快照、复制地址、下载 YAML、导入客户端；刷新来源与更新规则分别处理。
- **进阶配置**：倍率正则、重命名、代理组、额外域名、自定义规则与策略映射，保留协议字段和数值精度。

Vue 3 + Vite 前端提供配置分区、倍率分布与 AI 链路可视化。布局参考 [SubBoost](https://github.com/SubBoost/subboost) 的配置／预览组织方式，页面源码独立实现。生产资源内嵌在 Go 二进制中，无 CDN、登录或前端运行服务。

## Docker 部署

```bash
docker build -t clash-acl:local .
docker run -d --name clash-acl --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v clash-acl-data:/data \
  clash-acl:local
```

打开 `http://127.0.0.1:8080/`。Docker 自动构建前端和 Go，最终镜像只运行 Go 程序，以 `10001:10001` 用户运行。数据卷保留设置、来源缓存、快照及上游规则。

| 环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| `CLASH_ACL_LISTEN_ADDR` | 本地 `127.0.0.1:8080`；镜像 `0.0.0.0:8080` | HTTP 地址 |
| `CLASH_ACL_DATA_DIR` | 本地 `data`；镜像 `/data` | 配置与持久数据 |
| `CLASH_ACL_RULE_AUTO_UPDATE` | `true` | 24 小时检查 ACL；false 关闭定时，仍可手动更新 |

GitHub Actions 在推送 main 时发布 `ghcr.io/b1xcy/clash_acl:latest`；推送 v* 标签触发 release.yml，先发布并核验 GHCR 镜像，再发布 GitHub Release。v1.0 提供镜像标签 v1.0、1.0、latest 和 SHA，Release 名称 1.0；附件包含 Linux amd64 可执行包（内嵌页面／规则）、本标签完整源码 ZIP 和 SHA256SUMS。Release 创建使用 GITHUB_TOKEN，无需额外密钥，失败可在同一标签重新运行。

## 使用

1. 粘贴订阅地址或直接节点，等待自动解析与聚合。来源可逐个停用，关闭聚合时只使用第一条订阅。
2. 在直接节点列表勾选自建（0 倍率）。支持 SS、TUIC、VLESS、VMess、Trojan、Hysteria2/hy2、AnyTLS 混合输入。
3. 多选 AI 出口，使用上下移动调整顺序，需要时启用链式代理。内置规则在 placeholder 和完整清单中显示；额外域名框留空即可，只补中转站等其他服务。
4. 选择 ACL4SSR 方案，必要时进入进阶配置。预览实际组后生成，再复制、下载或导入客户端。

生成地址固定当次设置，修改后需要重新生成。客户端更新可选择刷新来源。普通下载读取快照，规则版本改变则使用保存节点重建。上游失败但有缓存时明确标记使用旧缓存。

AI 链路为前置节点 → 所选出口 → AI。前置选择默认使用 `⚡ AI 前置自动`（url-test，300 秒间隔、50ms 容差），选项依次为 AI 前置自动、DIRECT、其余节点（DIRECT 直接连接所选 AI 出口）；自动候选排除所有勾选出口和 DIRECT，手动节点候选同样排除所有勾选出口，保留原候选的倍率顺序（AI 前置自动包含高倍率，普通组策略不变）。最终 `🤖 AI 固定出口` 组支持多个指定节点，按勾选顺序排列，可在客户端切换。

前置测速 URL 留空时使用首个已选 VPS 的 `https://服务器/`，使用标准 HTTPS 端口，不套用代理协议端口；IPv6 自动加方括号。可手动填写真实 HTTP(S) 健康检查地址，首个 VPS 需提供相应网页服务。测速只在客户端进行；首个出口是生成时的固定测速基准，客户端切换最终出口不会自动改变该 URL。缺少出口／入口或存在循环时拒绝生成。TUIC/Hysteria2 需要支持 UDP 转发的前置节点，Claude IP-ASN 依赖客户端 ASN 数据库。服务器不做节点连通性、测速或出口 IP 检测。

AI 补充参考 [Net.Coffee GPT](https://ip.net.coffee/gpt/site.html) 与 [Claude](https://ip.net.coffee/claude/site.html)，补域名、CDN、认证、语音、遥测及 Claude IP/ASN，随镜像发布；ACL 上游更新不会覆盖。可选 geosite/NTP 未默认启用。none 方案保留已启用的 AI／电报服务分流。

订阅生成统一覆写 DNS 为 fake-ip：默认通过主代理查询 Cloudflare／Google DoH，绑定 AI 出口时统一通过 AI 出口查询；直连目标与代理节点域名单独使用阿里 DoH，避免节点解析循环。不使用系统 DNS 或国内外并发 fallback；基础配置中的 DNS 会被替换。旧订阅地址下次下载自动重建，无需刷新来源。

17 个 ACL4SSR 方案另附 806 条字节国内域名规则，覆盖抖音／火山／汽水、头条／西瓜、番茄、飞书、掘金、懂车帝及火山引擎等，包括 `bytegecko.com`／`bytegeckoext.com` 和 `byte-test.com`。补充规则跟随方案的国内直连策略，位于原广告拦截规则之后、国内 IP 判断之前，保留自定义规则和 AI 固定出口优先级；none 方案不注入。来源为 [V2Fly domain-list-community 固定快照](https://github.com/v2fly/domain-list-community/tree/dd4e3c080cee602512768e8109f89fafaa94b8ca/data)，排除海外服务 include、`@!cn` 与广告标记，未将 TikTok、CapCut 国际版、Lark 国际版及 BytePlus 测试域名 `byteperf.com` 并入直连。补充随程序发布，不会被 ACL4SSR 更新覆盖；更新程序后旧订阅下载自动应用，不修改客户端现用配置。

生成配置包含 TUN 的 TCP／UDP 53 劫持、自动路由和严格路由参数，保留基础配置的 TUN 开关与其他设置；客户端需启用 TUN 并关闭冲突的 DNS 覆写。仅系统代理无法接管所有程序的 DNS，浏览器自带 DoH、WebRTC 或其他绕过客户端的流量也无法仅靠订阅保证。导入后可用 browserleaks.com/dns 实测，直连目标的国内 DNS 属于预期分流；静态配置校验不代表实际出口已验证。

## 项目结构

```text
cmd/clash-acl/          Go 入口
internal/app/          HTTP、导入、聚合、策略、快照、更新及相邻测试
internal/assets/       Go embed 资源入口
  rules/               ACL4SSR 与 AI 补充
  dist/                Vite 生成资源（不入库）
frontend/              Vue 3 + Vite 与 npm 依赖锁
  src/App.vue          页面区域
  src/useConverter.js  状态、异步导入与 API
  src/components/      节点表、倍率分布、AI 链路
docs/                  API、当前交接与历史
.github/workflows/     Docker 发布
```

## 本地开发

需要 Go 1.25.5+ 与 Node 22.18+／24+。先构建前端再运行 Go；dist 自动生成，洁净 checkout 不包含它。

```bash
npm --prefix frontend ci
npm --prefix frontend run build
go run ./cmd/clash-acl
```

热更新：保持 Go 服务运行，另一个终端运行 `npm --prefix frontend run dev`。Vite 将 API/下载转发到本地 Go。

```bash
npm --prefix frontend run build
go test ./...
go vet ./...
go build ./cmd/clash-acl
docker build -t clash-acl:local .
```

Python 辅助操作必须通过 uv；本项目不需要 Python。npm 提交 package-lock.json，Go 提交 go.mod/go.sum；不提交运行数据和真实订阅／节点凭据。

[API 与配置](docs/API.md) · [当前交接](docs/handoff.md) · [历史记录](docs/history.md) · [ACL 规则来源](internal/assets/rules/ACL4SSR/SOURCE.md)
