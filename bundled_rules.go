package main

import (
	"embed"
	"errors"
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
	{ID: "acl4ssr-online", Name: "ACL4SSR 默认版", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online.ini"},
	{ID: "acl4ssr-mini", Name: "ACL4SSR 精简版", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Mini.ini"},
	{ID: "acl4ssr-full-noauto", Name: "ACL4SSR 完整版（无自动测速）", file: "rules/ACL4SSR/Clash/config/ACL4SSR_Online_Full_NoAuto.ini"},
}

func loadRulePreset(id string) ([]externalRuleSet, []string, string, error) {
	for _, preset := range rulePresets {
		if preset.ID != id {
			continue
		}
		data, err := bundledRules.ReadFile(preset.file)
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

func readBundledRule(base, reference string) ([]byte, error) {
	file := path.Clean(path.Join(base, reference))
	if !strings.HasPrefix(file, "rules/ACL4SSR/Clash/") {
		return nil, errors.New("bundled rule path must stay inside the preset")
	}
	data, err := bundledRules.ReadFile(file)
	if err != nil {
		return nil, errors.New("bundled rule file not found")
	}
	return data, nil
}
