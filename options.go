package main

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

type ConversionOptions struct {
	RenameRules []RenameRule `json:"rename_rules"`
	RatePattern string       `json:"rate_pattern"`
	AI          AIPolicy     `json:"ai"`
	GroupsJSON  string       `json:"groups_json"`
}

func decodeOptions(reader io.Reader) (ConversionOptions, []map[string]any, error) {
	var options *ConversionOptions
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&options); err != nil || options == nil {
		return ConversionOptions{}, nil, errors.New("expected a conversion settings object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ConversionOptions{}, nil, errors.New("expected one conversion settings object")
	}
	for _, rule := range options.RenameRules {
		if rule.Match == "" {
			return ConversionOptions{}, nil, errors.New("rename match cannot be empty")
		}
		if rule.Regex {
			if _, err := regexp.Compile(rule.Match); err != nil {
				return ConversionOptions{}, nil, errors.New("invalid rename regular expression")
			}
		}
	}
	if options.RatePattern != "" {
		if _, err := ratePattern(options.RatePattern); err != nil {
			return ConversionOptions{}, nil, err
		}
	}
	if strings.TrimSpace(options.GroupsJSON) == "" {
		options.GroupsJSON = "[]"
	}
	decoder = json.NewDecoder(strings.NewReader(options.GroupsJSON))
	decoder.UseNumber()
	var groups []map[string]any
	if err := decoder.Decode(&groups); err != nil || groups == nil {
		return ConversionOptions{}, nil, errors.New("group settings must be a JSON array")
	}
	if err := decoder.Decode(&extra); err != io.EOF {
		return ConversionOptions{}, nil, errors.New("expected one group settings array")
	}
	for _, group := range groups {
		name, nameOK := group["name"].(string)
		kind, typeOK := group["type"].(string)
		if !nameOK || !typeOK || name == "" || kind == "" {
			return ConversionOptions{}, nil, errors.New("each group requires a name and type")
		}
	}
	return *options, groups, nil
}
