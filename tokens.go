package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
)

func resolveTokens(path, adminToken, downloadToken string) (string, string, error) {
	if adminToken != "" && downloadToken != "" {
		if adminToken == downloadToken {
			return "", "", errors.New("CLASH_ACL_ADMIN_TOKEN and CLASH_ACL_DOWNLOAD_TOKEN must be distinct")
		}
		return adminToken, downloadToken, nil
	}
	saved := make(map[string]string)
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &saved); err != nil || saved == nil {
			return "", "", errors.New("cannot read saved credentials: check tokens.json format")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", errors.New("cannot read saved credentials: check tokens.json permissions")
	}
	changed := false
	if adminToken == "" {
		if saved["admin_token"] == "" {
			saved["admin_token"] = rand.Text()
			changed = true
		}
		adminToken = saved["admin_token"]
	}
	if downloadToken == "" {
		if saved["download_token"] == "" {
			saved["download_token"] = rand.Text()
			changed = true
		}
		downloadToken = saved["download_token"]
	}
	if adminToken == downloadToken {
		return "", "", errors.New("CLASH_ACL_ADMIN_TOKEN and CLASH_ACL_DOWNLOAD_TOKEN must be distinct")
	}
	if changed {
		data, err := json.MarshalIndent(saved, "", "  ")
		if err != nil {
			return "", "", errors.New("cannot encode generated credentials")
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			return "", "", errors.New("cannot save generated credentials: check data directory permissions")
		}
	}
	return adminToken, downloadToken, nil
}
