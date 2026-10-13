package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestConverterPageAndStatusPrivacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := defaultConfig()
	config.Sources = []Source{{ID: "test", Name: "Test source", URL: "https://example.invalid/sub?token=private-test-value", Enabled: true}}
	if err := writeConfig(path, config); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(path, "admin-test", "download-test")
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "订阅转换") || strings.Contains(page.Body.String(), "功能进度") {
		t.Fatal("embedded converter form not served")
	}
	for _, token := range []string{"", "download-test", "admin-test"} {
		request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if token == "admin-test" {
			if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Test source") || strings.Contains(response.Body.String(), "private-test-value") || strings.Contains(response.Body.String(), "https://") {
				t.Fatal("status response missing source or exposing its URL")
			}
		} else if response.Code != http.StatusUnauthorized {
			t.Fatal("status endpoint requires an admin credential")
		}
	}
}
