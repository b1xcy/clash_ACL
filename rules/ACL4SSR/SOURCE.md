# ACL4SSR 内置规则

来源：https://github.com/ACL4SSR/ACL4SSR

快照提交：802e3abf065f8609e7d9073ba506158dbe2ba6e7

导入日期：2026-10-14

保留上游规则文件和模板注释；模板中引用本仓库规则的远程地址改为相对本地路径，其他 INI 写法保留。更新规则时重新导入并构建镜像，运行时不联网拉取内置方案。

模板目录对齐 https://acl4ssr-sub.github.io/ 的远程配置列表（2026-10-14 部署脚本 js/693.4b28d9e1.js），仅导入 ACL4SSR 在线系列；后端相对 config/ 地址及第三方模板不属于此次导入范围。全部新增文件继续使用上述同一快照，避免不同版本混用。

模板共 17 个：

- ACL4SSR_Online.ini
- ACL4SSR_Online_AdblockPlus.ini
- ACL4SSR_Online_MultiCountry.ini
- ACL4SSR_Online_NoAuto.ini
- ACL4SSR_Online_NoReject.ini
- ACL4SSR_Online_Mini.ini
- ACL4SSR_Online_Mini_AdblockPlus.ini
- ACL4SSR_Online_Mini_NoAuto.ini
- ACL4SSR_Online_Mini_Fallback.ini
- ACL4SSR_Online_Mini_MultiMode.ini
- ACL4SSR_Online_Mini_MultiCountry.ini
- ACL4SSR_Online_Full.ini
- ACL4SSR_Online_Full_MultiMode.ini
- ACL4SSR_Online_Full_NoAuto.ini
- ACL4SSR_Online_Full_AdblockPlus.ini
- ACL4SSR_Online_Full_Netflix.ini
- ACL4SSR_Online_Full_Google.ini

规则文件数：40。新增 BanEasyList、BanEasyListChina、BanEasyPrivacy、ChinaIp、AmazonIp、Google、GoogleCN 与 GoogleCNProxyIP；保留模板注释引用的 ChinaIp 文件。
