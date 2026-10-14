package main

import (
	"embed"
	"errors"
	"io/fs"
	"path"
	"strings"
)

//go:embed rules/ACL4SSR
var bundledRules embed.FS

type RulePreset struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	file string
}

var rulePresets = []RulePreset{
	{ID: "acl4ssr-online", Name: "ACL4SSR_Online 默认版 分组比较全", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online.ini"},
	{ID: "acl4ssr-mini", Name: "ACL4SSR_Online_Mini 精简版", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini.ini"},
	{ID: "acl4ssr-full-noauto", Name: "ACL4SSR_Online_Full_NoAuto.ini 全分组 无自动测速 重度用户使用", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full_NoAuto.ini"},
	{ID: "acl4ssr-adblock-plus", Name: "ACL4SSR_Online_AdblockPlus 更多去广告", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_AdblockPlus.ini"},
	{ID: "acl4ssr-multicountry", Name: "ACL4SSR_Online_MultiCountry 多国分组", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_MultiCountry.ini"},
	{ID: "acl4ssr-noauto", Name: "ACL4SSR_Online_NoAuto 无自动测速", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_NoAuto.ini"},
	{ID: "acl4ssr-noreject", Name: "ACL4SSR_Online_NoReject 无广告拦截规则", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_NoReject.ini"},
	{ID: "acl4ssr-mini-adblock-plus", Name: "ACL4SSR_Online_Mini_AdblockPlus.ini 精简版 更多去广告", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini_AdblockPlus.ini"},
	{ID: "acl4ssr-mini-noauto", Name: "ACL4SSR_Online_Mini_NoAuto.ini 精简版 不带自动测速", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini_NoAuto.ini"},
	{ID: "acl4ssr-mini-fallback", Name: "ACL4SSR_Online_Mini_Fallback.ini 精简版 带故障转移", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini_Fallback.ini"},
	{ID: "acl4ssr-mini-multimode", Name: "ACL4SSR_Online_Mini_MultiMode.ini 精简版 自动测速、故障转移、负载均衡", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini_MultiMode.ini"},
	{ID: "acl4ssr-mini-multicountry", Name: "ACL4SSR_Online_Mini_MultiCountry.ini 精简版 带港美日国家", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini_MultiCountry.ini"},
	{ID: "acl4ssr-full", Name: "ACL4SSR_Online_Full 全分组 重度用户使用", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full.ini"},
	{ID: "acl4ssr-full-multimode", Name: "ACL4SSR_Online_Full_MultiMode.ini 全分组 多模式 重度用户使用", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full_MultiMode.ini"},
	{ID: "acl4ssr-full-adblock-plus", Name: "ACL4SSR_Online_Full_AdblockPlus 全分组 重度用户使用 更多去广告", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full_AdblockPlus.ini"},
	{ID: "acl4ssr-full-netflix", Name: "ACL4SSR_Online_Full_Netflix 全分组 重度用户使用 奈飞全量", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full_Netflix.ini"},
	{ID: "acl4ssr-full-google", Name: "ACL4SSR_Online_Full_Google 全分组 重度用户使用 谷歌细分", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full_Google.ini"},
}

func ruleLibrary(libraries []fs.FS) fs.FS {
	if len(libraries) > 0 && libraries[0] != nil {
		return libraries[0]
	}
	return bundledRules
}

func loadRulePreset(id string, libraries ...fs.FS) ([]externalRuleSet, []string, string, error) {
	for _, preset := range rulePresets {
		if preset.ID != id {
			continue
		}
		data, err := fs.ReadFile(ruleLibrary(libraries), preset.file)
		if err != nil {
			return nil, nil, "", errors.New("cannot read bundled rule template")
		}
		sets, err := parseRuleConfig(data)
		if err != nil {
			return nil, nil, "", err
		}
		groups := []string{}
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if value, found := strings.CutPrefix(line, "custom_proxy_group="); found {
				name, _, _ := strings.Cut(value, "`")
				if name != "" {
					groups = append(groups, name)
				}
			}
		}
		return sets, groups, path.Dir(preset.file), nil
	}
	return nil, nil, "", errors.New("unknown bundled rule preset")
}

func readBundledRule(base, reference string, libraries ...fs.FS) ([]byte, error) {
	file := path.Clean(path.Join(base, reference))
	if !strings.HasPrefix(file, "rules/ACL4SSR/Clash/") {
		return nil, errors.New("bundled rule path must stay inside the preset")
	}
	data, err := fs.ReadFile(ruleLibrary(libraries), file)
	if err != nil {
		return nil, errors.New("bundled rule file not found")
	}
	return data, nil
}
