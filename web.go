package main

import _ "embed"

//go:embed web/index.html
var indexHTML []byte

type Feature struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

func featureProgress() []Feature {
	return []Feature{
		{"configuration", "基础服务与配置存储", "done", "访问令牌、配置读写与持久化已可用"},
		{"dashboard", "进度工作台", "done", "查看运行状态、来源数量与功能进度"},
		{"sources", "YAML 来源读取与缓存", "pending", "下一步支持订阅刷新与失败缓存"},
		{"import", "多行协议链接导入", "pending", "混合协议、逐行预览与错误反馈"},
		{"aggregation", "聚合、倍率与重命名", "pending", "统一节点身份、三档倍率与名称处理"},
		{"policies", "分组编辑与 AI 固定出口", "pending", "统一代理组和域名规则，约束自建出口"},
		{"download", "配置生成与下载", "pending", "固定订阅地址与强制刷新"},
		{"docker", "Docker 运行与镜像构建", "done", "非 root 运行、数据卷持久化及 GHCR workflow"},
	}
}
