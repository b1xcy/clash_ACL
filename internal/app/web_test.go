package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
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
	handler := newHandler(path)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "订阅转换") || strings.Contains(page.Body.String(), "功能进度") {
		t.Fatal("embedded converter form not served")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Test source") || strings.Contains(response.Body.String(), "private-test-value") || strings.Contains(response.Body.String(), "https://") {
		t.Fatal("status response missing source or exposing its URL")
	}
	if strings.Contains(page.Body.String(), "connectForm") || strings.Contains(page.Body.String(), "adminToken") {
		t.Fatal("converter page still requires a token")
	}
	for _, asset := range regexp.MustCompile(`/assets/[^" ]+`).FindAllString(page.Body.String(), -1) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, asset, nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("embedded frontend asset unavailable: %s", asset)
		}
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/rules/ai", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "claudemcpcontent.com") || !strings.Contains(response.Body.String(), "crixet.com") || !strings.Contains(response.Body.String(), "api.githubcopilot.com") {
		t.Fatal("AI placeholder list is incomplete")
	}
}
