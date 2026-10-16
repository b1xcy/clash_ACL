# 开发交接

最后更新：2026-10-16。

## 字节国内域名补全

- 用户反馈抖音资源 lf-normal-gr-sourcecdn.bytegecko.com 落入代理兜底。新增 rules/Domestic/ByteDance.list 共 806 条域名规则；来源 V2Fly domain-list-community 固定提交 dd4e3c080cee602512768e8109f89fafaa94b8ca 的 bytedance、douyin、doubao、fqnovel、juejin、feishu、volcengine、dongchedi，保留 MIT LICENSE。普通域名转 DOMAIN-SUFFIX、full: 转 DOMAIN，排除 include 行、@!cn、@ads；未展开 TikTok／海外 AI／Lark／Trae 等海外或混合 include。
- 补齐 bytegecko／bytegeckoext、字节 CDN／静态／视频资源、抖音／火山／汽水、头条／西瓜、番茄、飞书、掘金、懂车帝和火山引擎等。跟随每个方案 ChinaDomain 的最终映射目标，在原 ChinaDomain 后注入，保留此前广告拦截、用户自定义与 AI 固定出口优先级；全部 17 个方案有效，none 不注入。不修改上游 ACL4SSR 文件，运行时上游更新不会覆盖程序内置补充。快照版本追加 domestic-v1，更新程序后旧订阅下载用保存节点重建。
- Cloudflare 中国测试标签不能证明 cloudflareperf.com 全域仅国内；实时连接观察到该主机使用 172.64.155.229／104.18.32.27，未添加其全域或单主机强制直连。byteperf.com 也未加入国内规则，避免影响字节海外测试。
- 验证：17 个方案回归通过，覆盖用户报告的完整主机名、相关国内域名首个域名规则命中、用户优先、广告顺序及海外服务排除；Go 全量 test/build/vet、Docker build 与 diff --check 通过。使用本机 Mihomo Meta alpha-e4dd968（2026-10-08，go1.26.8，with_gvisor）及 Country／ASN 数据，仅以合成节点执行 CLASH_ACL_TEST_MIHOMO / CLASH_ACL_TEST_MIHOMO_DATA 环境变量下的 go test ./internal/app -run TestGeneratedConfigurationsWithMihomo -count=1，17 个方案配置校验通过。未修改或重载现用客户端配置，未验证真实节点出口；本次仅本地提交，不自动发布或推送新标签。

## v1.2.2 发布

- 用户授权打标签并推送；本版包含 DNS 防泄露配置与旧快照重建。前端顶部硬编码显示 v1.2.2，不新增 API，Release 说明同步更新。
- 验证：Vite build、Go 全量 test/build/vet、docker build -t clash-acl:local .、git diff --check 通过；构建后的前端 JS 已确认包含 v1.2.2。实际 DNS 泄露及 Mihomo 校验边界沿用下节记录。
- 已将 main 与 v1.2.2 原子推送，标签指向 9cf3865adbc8f715b097924ffbbc88ef4e50e0d3。[Release](https://github.com/b1xcy/clash_ACL/releases/tag/v1.2.2) 正式发布，Release Actions 38751377742 与 main 镜像 Actions 38751378402 均成功；三个附件已通过 API 确认存在（Linux amd64 包、完整源码 ZIP、SHA256SUMS），本次未下载附件复验。GHCR v1.2.2 manifest 可读取，摘要 sha256:3ab73a2fc3684b3b58308995c340d23aea779ef13db9fa8eac8ffd22a038996d。后续仅提交发布验收记录，不移动标签。

## 订阅 DNS 防泄露

- 已阅读用户 data.txt 中的 Shadowrocket 讨论（topic/2785002，12 条帖子），并按要求删除该未跟踪文件。可借鉴的是代理与直连 DNS 分离，现有 nameserver／direct-nameserver 已实现；不添加 Shadowrocket 专用 dns-direct-system 字段，不改回系统 DNS。帖中仍有失败反馈，不能据此宣称通过实际泄露检测；出现国内解析器需要结合所查询域名及路由判断，不能一概视为泄露。本次仅补充评估记录，没有修改代码，无需重复代码构建检查。
- 生成阶段统一替换基础 DNS：启用 fake-ip，Cloudflare／Google DoH 显式绑定主代理；选择 AI 固定出口时默认查询统一绑定 AI 组，不因 DNS 故障退回系统解析。节点域名与 DIRECT 目标使用独立阿里 DoH，避免解析代理自身的循环。此策略也意味着 AI 出口故障会影响默认 DNS 可用性。
- 补齐 TUN TCP／UDP 53 劫持、auto-route、strict-route、auto-detect-interface，保留原开关和其他设置，不自动开启 TUN。客户端需开启 TUN、关闭冲突 DNS 覆写；系统代理、浏览器独立 DoH、WebRTC 等绕行仍需客户端处理，不能声称彻底消除所有泄露。
- 快照生成版本改为 dns-v1，旧下载地址用保存节点重建，无需刷新来源。README 已说明行为与验证边界。
- 验证：go test ./...、go build ./...、go vet ./...、docker build -t clash-acl:local . 通过。回归覆盖基础 DNS 覆写、普通／AI 默认 DNS 绑定、独立解析、TUN 参数保留及输入不变。Mihomo 官方 v1.19.32 Windows 内核下载无响应，已中止，未完成内核校验；未执行真实节点连通性或泄露网站验收。下一步在实际客户端版本开启 TUN 后检测 DNS 出口。仅本地提交，不 push／发布。

## v1.2.1 前置选项排序

- 按用户要求将 `🔗 AI 前置选择` 顺序调整为 `⚡ AI 前置自动`、`DIRECT`、其余节点，方便切换直连；自动组候选保持原样。
- 快照生成版本更新为 ai-self-v4，旧订阅下载时从保存节点重建以应用新顺序。相关现有测试、README、开发约定与 Release 说明同步调整。用户已授权打 v1.2.1 标签并推送发布。
- 验证：Go 全量 test/build/vet、Vite build、`docker build -t clash-acl:local .` 与 git diff --check 通过；本次未配置 Mihomo 内核，不做真实连通性检测。
- 已原子推送 main 与 v1.2.1，标签指向 2e92e7918434f1025bbe82856f17221fc04ede60。[Release 1.2.1](https://github.com/b1xcy/clash_ACL/releases/tag/v1.2.1) 正式发布，[Release Actions](https://github.com/b1xcy/clash_ACL/actions/runs/38606988485) 与 main 镜像 Actions 38606988302 均成功。
- GHCR v1.2.1、1.2.1、发布时 latest 均匿名访问 HTTP 200，摘要 sha256:01a9a691666e28b7fcf993efc0949e037280ada2cd5a37ea9549210796a05514。三个附件已下载，两个压缩包的 SHA256 校验通过，源码 ZIP 覆盖标签全部 116 个跟踪文件。

## AI 前置直连选项

- `🔗 AI 前置选择` 末尾增加 `DIRECT`，选择后直接连接所选 AI 出口；默认仍为 `⚡ AI 前置自动`。DIRECT 不参与自动测速，最终 AI 出口组仍仅含指定节点。
- 快照生成版本更新为 ai-self-v3，旧订阅下载使用已保存节点重建并获得直连选项，无需刷新来源。
- 已更新现有多出口、AnyTLS 链式及全部模板自建出口测试预期。Go 全量 test/build/vet、Vite build、`docker build -t clash-acl:local .`、git diff --check 通过；本次环境未配置 Mihomo 内核，未运行内核配置校验。不做真实连通性检测。仅本地提交，不 push 或发布。

## v1.2 发布

- 用户已授权打版本标签并推送发布，本次使用 v1.2，包含 AI 前置 DIRECT 选项和旧订阅快照重建。Release 说明同步加入此项能力，沿用 GHCR 镜像、Linux amd64 包、完整源码 ZIP 和 SHA256SUMS 发布流程。
- 已原子推送 main 与 v1.2，标签指向 bc6f12f96a7393a9c376a9cf6737cb1711e5993b。[Release 1.2](https://github.com/b1xcy/clash_ACL/releases/tag/v1.2) 正式发布；[Release Actions](https://github.com/b1xcy/clash_ACL/actions/runs/38604349414) 与 main 镜像 Actions 38604348669 均成功。
- GHCR v1.2、1.2 及发布时 latest 均匿名拉取 HTTP 200，摘要 sha256:725556a9034ae0180d1560cf4aa9bbaf4f364ec40d58df7b905c2e7712be9734。三个附件已实际下载，两个压缩包 SHA256SUMS 校验通过，源码 ZIP 覆盖标签全部 116 个跟踪文件。保留旧版本，VPS 未修改。

## 按来源过滤 HTTP/SOCKS

- 用户纠正原过滤范围：仅去除 HTTP/SOCKS，VLESS 等其他协议保留。原实现按加密状态扩大至 VLESS/VMess/SS 已撤回；现在按类型去除 http/socks4/socks5，包含 TLS 变体，页面明确写协议名称，不把保留的公共节点视为可信。
- 节点总览提供默认策略（默认关闭），每个已读取订阅旁提供“跟随默认／丢弃／保留”。policy.drop_plaintext 历史字段保留，source_drop_plaintext 为稳定来源 ID → bool 覆盖，false 明确保留。设置可保存／重载，修改自动聚合，换订阅顺序保持来源策略；修改后重新生成订阅。
- 同一节点在多来源去重后仅移除要求丢弃的来源记录；允许来源仍保留，倍率／名称仅依据保留来源。filtered_count 统计实际删除的去重节点数，原缓存不变。AI 选中项被过滤时明确缺失报错，不回退；全部节点过滤时拒绝生成。
- 默认策略与来源覆盖固定于快照参数，refresh 沿用，规则重建用已过滤节点；旧配置无字段时仍关闭。既有 v1.0 标签与 Release 保留。
- 验证：Go 全量 test/build/vet、Vite build、Docker build 通过。单元／接口测试覆盖其他协议保留、来源 true/false 优先、重复来源移除与倍率恢复、保存设置、快照固定、生成／聚合一致。最终 Docker 镜像使用两个合成订阅验收默认关闭但公开订阅丢弃／私有订阅保留、订阅重排、重复节点仅保留允许来源、HTTP TLS 节点按类型移除、无 TLS VLESS 保留、保存／重启与订阅 refresh、下载 YAML 和内嵌来源选择控件。临时容器／订阅服务已清理，仅本地提交，未 push 或发布镜像。
## 当前范围

用户已提出的功能均已实现：七种协议链接／YAML 订阅导入、自动聚合去重、倍率与重命名、国旗、自建零倍率、17 个 ACL4SSR 方案、AI 固定出口及链式前置选择、电报自建选项、规则每 24 小时更新、完整生成与持久化下载。

用户最新明确：真实节点/VPS 连通性与出口 IP 无需验收，也不在服务器实现检测，不再作为未完成项目。历史中的 501、认证、手动确认或自建 JSON 页面均已被后续需求取代，历史仅用于追溯。

## 本次整理与前端

- Go 入口 cmd/clash-acl，业务和测试 internal/app，规则与生产前端 internal/assets embed。frontend 存前端，docs 存文档，根目录保留项目配置与 README/agent。
- AI placeholder 从 GET /api/rules/ai 获取当前 ACL AI/OpenAi 与 NetCoffee 补充，规则更新后重新读取。提示和完整清单仅显示，不写入额外域名；留空即可。
- Vue 3 + Vite 重做页面：分区导航、来源选择、自动导入、搜索节点表、倍率分布、AI 链路图、规则更新、进阶编辑和导出／实际组预览。参考 SubBoost 的配置／预览组织方式，未复制代码或加入其账号／数据库功能。
- 仅 Vue 运行依赖，Vite 和 Vue 插件构建依赖；无路由、状态库、图表库或 CDN。提交 package-lock，Node 只参与构建，生产仍 scratch 非 root Go 进程。
- Vite 输出 internal/assets/dist，不入库；洁净 checkout 先 npm ci/build，再运行 Go。Docker 前端阶段自动生成资源，发布 CI 沿用 Docker。
- 既有 API、请求结构、节点 ID、groups_json 精度、快照地址和更新数据保持兼容；未重置用户运行数据。

## AI 多出口与前置自动测速

- 用户明确调整：允许多选最终出口；前置优先 url-test。用户确认测速 URL 指首个出口 VPS 的 HTTP(S) 地址，可手动修改。
- ai.node_ids 按选择顺序指定出口，解析稳定 ID／旧保存别名并按连接去重；旧 node_id 在 node_ids 为 null／缺失时兼容，空数组明确清除，持久化不会恢复旧绑定。任一候选缺失都拒绝生成，AI 组不加入普通选择或 DIRECT。
- 全部出口写入 dialer-proxy: 🔗 AI 前置选择。前置手动组首项 ⚡ AI 前置自动（url-test，300s、50ms），其余为手动节点；自动与手动均排除所有最终出口和虚拟节点。延用原候选按倍率顺序排列，AI 自动也包含高倍率；普通倍率策略不变。该 AI 显式自动组不受 ACL NoAuto 模板限制。
- ai.test_url 留空时派生首个出口 https://服务器/，使用标准 HTTPS 端口，不套用代理端口；IPv6 正确括号化。可手动指定真实 HTTP(S) 健康检查 URL；VPS 必须提供对应网页服务。生成时固定首个基准，客户端切换最终出口不会更换测速 URL。只生成配置、不做服务器网络检测。
- Vue 多选面板提供搜索、候选复选框、顺序上下移动、缺失项保留与移除；保存多选及 URL，预览显示实际测速地址。AI 图示区分前置自动／手动和多个出口。旧订阅地址按 ai-self-v2 重建自动组，不拉取来源。
- Go 全量测试（包含 Mihomo v1.19.32 配置校验）、build/vet、相关 race、Vite 与 Docker 构建已通过。新增测试覆盖多出口去重／顺序、全部出口排除、每节点 dialer-proxy、自动／手动组、URL覆盖、IPv6、缺失节点、无前置、旧配置／清空和快照重建。
- 最终 Docker 镜像的 Chrome 验收多选、顺序、自动与手动组、首个 URL 派生／重排、URL覆盖、保存／重载、非法 URL 和第二出口缺失拒绝、旧地址下载、Mihomo 配置校验及桌面／手机布局；零 JS 异常，来源仅读取一次。临时进程和容器已清理。不执行实际节点连通性、出口 IP 或服务端测速。

## 验证

- npm build、Go 全量 test/vet/build、相关 race、git diff --check 通过。Go 全量测试启用本机 Mihomo Meta v1.19.32，验证全部方案、七协议直连／链式与自建电报规则；只做配置校验，不检测真实连通性或 IP。
- Docker 最终构建通过，包括洁净构建阶段的 npm ci、Vite 资源及 Linux Go 测试；运行镜像 15,854,396 bytes，用户 10001:10001，无 Node 运行进程。
- Chrome 在最终 Docker 镜像中验收自动读取／解析／聚合、失败行、自建标记与取消、国旗、Mini 电报选项、AI 链式、立即生成等待新输入、缺失绑定拒绝、旧快照、来源勾选缓存复用、节点搜索、重命名、高级规则、大整数精度、设置保存／重载、复制、下载刷新地址。来源只拉取一次，零 JS 异常。
- AI 输入 placeholder 读取实际内置清单，验证 Copilot、Claude MCP 等规则已显示；字段保持为空，保存后的 ai.domains 仍是空数组。1440px 桌面及 390px 手机截图已目视检查，无横向溢出。浏览器测试容器／进程已清理。
- dist、node_modules 与本地二进制均已忽略；历史交接归档。当前没有本次范围内的未完成实现；不自动 push 或发布 GHCR。

## 接手约定

先阅读根目录 agent.md、README.md 与本文。历史可选规划不作为必做功能；不增加服务器节点检测。Python 一律 uv，不提交真实凭据或改生效 Clash 配置。默认不 push；本次用户明确授权 v1.0 标签与完整 main 推送及 Release/GHCR 发布。

## v1.0 发布

- 新增 release.yml：v* 标签触发，可在标签上手动重跑；先复用 docker-image.yml 发布并按摘要核验 GHCR，再构建前端、运行 Go test/vet、打包 Linux amd64 二进制、完整源码 ZIP 与 SHA256SUMS，发布标题为去 v 的版本号的 GitHub Release。同标签重跑更新附件，不创建重复 Release。
- docker-image.yml 增加 workflow_call 与摘要输出；main 自动镜像发布保持，标签改由 Release 调用，避免同标签重复构建；v1.0 镜像提供 v1.0、1.0、latest、SHA。使用标准 GITHUB_TOKEN 的 contents:write/packages:write，无额外凭据。
- actionlint 与本地 Docker 构建通过。已按授权原子推送完整 main 与 v1.0，标签指向 e8ca195901f0c9396fd63c91d7554dc235e76e6e，仓库保持私有。首次标签自动运行未出现，已在 v1.0 手动触发同一 Release workflow。
- GitHub Release [1.0](https://github.com/b1xcy/clash_ACL/releases/tag/v1.0) 已正式发布（非 draft/prerelease）；[Release Actions](https://github.com/b1xcy/clash_ACL/actions/runs/38538029002) 与 main 镜像发布均成功。镜像日志确认 ghcr.io/b1xcy/clash_acl:v1.0、:1.0、:latest、:sha-e8ca195 已发布，摘要 sha256:f37762ebd455b0982931cfe32d5f9621614285dac0aa3172d3bff60caabe891a 经 registry inspect 核验。
- Release 三个附件：clash-acl-v1.0-linux-amd64.tar.gz、clash-acl-v1.0-source.zip、SHA256SUMS。已实际下载并核对 SHA256SUMS；源码包含完整标签源码，包括 Go/Vue/规则/依赖锁和 workflow。GitHub 包 API 因当前本机凭据缺少包读取范围返回 403，镜像发布／摘要通过 Actions 的 GITHUB_TOKEN 验证；没有更改访问权限或打印凭据。

历史见 [history.md](history.md)，API 和生成约束见 [API.md](API.md)。

## v1.1 发布

- 用户已明确授权创建 v1.1 并 push 发布；版本包含 HTTP/SOCKS 按来源过滤与范围修正。Release workflow 说明已增加来源过滤功能，沿用完整源码、Linux amd64 可执行包、SHA256SUMS 和 GHCR 版本镜像发布流程。
- 已原子推送 main 和 v1.1，标签指向 b7063ff1b8fc32fb63b0f1223288a78934cc467e。[Release 1.1](https://github.com/b1xcy/clash_ACL/releases/tag/v1.1) 正式发布（非 draft/prerelease），[Release Actions](https://github.com/b1xcy/clash_ACL/actions/runs/38549863455) 与 main 镜像 Actions 38549863008 均成功。
- GHCR v1.1、1.1 镜像摘要 sha256:063d0dbc2238c9954cfbc93213540478f8bb430edf6ecf1d035a27750161b299；匿名获取两个版本标签和发布时 latest 均 HTTP 200。仓库当前公开，包也已允许匿名拉取。
- 三个附件已实际下载；Linux amd64 包与源码 ZIP 的 SHA256SUMS 均核验通过，源码 ZIP 包含标签下完整 116 个跟踪文件。v1.0 标签／Release 保留，用户数据及 VPS 未修改。
