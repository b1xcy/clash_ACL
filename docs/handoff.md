# 开发交接

最后更新：2026-10-14。

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
