# 开发交接

最后更新：2026-10-14。

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

## 验证

- npm build、Go 全量 test/vet/build、相关 race、git diff --check 通过。Go 全量测试启用本机 Mihomo Meta v1.19.32，验证全部方案、七协议直连／链式与自建电报规则；只做配置校验，不检测真实连通性或 IP。
- Docker 最终构建通过，包括洁净构建阶段的 npm ci、Vite 资源及 Linux Go 测试；运行镜像 15,854,396 bytes，用户 10001:10001，无 Node 运行进程。
- Chrome 在最终 Docker 镜像中验收自动读取／解析／聚合、失败行、自建标记与取消、国旗、Mini 电报选项、AI 链式、立即生成等待新输入、缺失绑定拒绝、旧快照、来源勾选缓存复用、节点搜索、重命名、高级规则、大整数精度、设置保存／重载、复制、下载刷新地址。来源只拉取一次，零 JS 异常。
- AI 输入 placeholder 读取实际内置清单，验证 Copilot、Claude MCP 等规则已显示；字段保持为空，保存后的 ai.domains 仍是空数组。1440px 桌面及 390px 手机截图已目视检查，无横向溢出。浏览器测试容器／进程已清理。
- dist、node_modules 与本地二进制均已忽略；历史交接归档。当前没有本次范围内的未完成实现；不自动 push 或发布 GHCR。

## 接手约定

先阅读根目录 agent.md、README.md 与本文。历史可选规划不作为必做功能；不增加服务器节点检测。Python 一律 uv，不提交真实凭据或改生效 Clash 配置。交付包含验证、交接和本地 commit，不自动 push。

历史见 [history.md](history.md)，API 和生成约束见 [API.md](API.md)。
