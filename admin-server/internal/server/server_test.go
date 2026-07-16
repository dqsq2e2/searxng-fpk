package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	pngcodec "image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusRequiresAdministrator(t *testing.T) {
	handler := newTestHandler(t, "/app/searxng-admin")

	tests := []struct {
		name        string
		headerValue string
		wantStatus  int
	}{
		{name: "missing header", wantStatus: http.StatusForbidden},
		{name: "false header", headerValue: "false", wantStatus: http.StatusForbidden},
		{name: "wrong case", headerValue: "True", wantStatus: http.StatusForbidden},
		{name: "administrator", headerValue: "true", wantStatus: http.StatusOK},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://nas.example/app/searxng-admin/api/status", nil)
			if test.headerValue != "" {
				request.Header.Set(adminHeader, test.headerValue)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestStatusUsesCompleteGatewayPrefix(t *testing.T) {
	handler := newTestHandler(t, "/app/custom-admin")

	request := httptest.NewRequest(http.MethodGet, "http://nas.example/app/custom-admin/api/status", nil)
	request.Header.Set(adminHeader, "true")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("prefixed status = %d, want %d", response.Code, http.StatusOK)
	}

	var status statusResponse
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if status.Status != "ok" || status.Version != "test-version" {
		t.Fatalf("unexpected status payload: %+v", status)
	}
	if status.ServiceURL != "http://nas.example:8888" {
		t.Fatalf("serviceUrl = %q, want %q", status.ServiceURL, "http://nas.example:8888")
	}

	outsideRequest := httptest.NewRequest(http.MethodGet, "http://nas.example/api/status", nil)
	outsideRequest.Header.Set(adminHeader, "true")
	outsideResponse := httptest.NewRecorder()
	handler.ServeHTTP(outsideResponse, outsideRequest)
	if outsideResponse.Code != http.StatusNotFound {
		t.Fatalf("unprefixed status = %d, want %d", outsideResponse.Code, http.StatusNotFound)
	}

	partialRequest := httptest.NewRequest(http.MethodGet, "http://nas.example/app/custom-admin-extra/api/status", nil)
	partialRequest.Header.Set(adminHeader, "true")
	partialResponse := httptest.NewRecorder()
	handler.ServeHTTP(partialResponse, partialRequest)
	if partialResponse.Code != http.StatusNotFound {
		t.Fatalf("partial-prefix status = %d, want %d", partialResponse.Code, http.StatusNotFound)
	}
}

func TestStaticSPAAllowsAuthenticatedNonAdministrator(t *testing.T) {
	handler := newTestHandler(t, "/app/searxng-admin")
	request := httptest.NewRequest(http.MethodGet, "http://nas.example/app/searxng-admin/settings/profile", nil)
	request.Header.Set(adminHeader, "false")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != "<html>SPA</html>" {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func TestStaticPathTraversalIsRejected(t *testing.T) {
	handler := newTestHandler(t, "/app/searxng-admin")
	request := httptest.NewRequest(http.MethodGet, "http://nas.example/app/searxng-admin/placeholder", nil)
	request.URL.Path = "/app/searxng-admin/../../secret.txt"
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestAllAPIEndpointsRequireAdministrator(t *testing.T) {
	handler, _, _ := newTestEnvironment(t, "/app/searxng-admin")
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/config", ""},
		{http.MethodPut, "/api/config", `{}`},
		{http.MethodGet, "/api/config/raw", ""},
		{http.MethodPut, "/api/config/raw", `{}`},
		{http.MethodPost, "/api/branding/logo", "invalid"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, "http://nas.example/app/searxng-admin"+test.path, strings.NewReader(test.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d, want %d", test.method, test.path, response.Code, http.StatusForbidden)
		}
	}
}

func TestGetAndPutConfigPreservesSecretAndUnknownFields(t *testing.T) {
	handler, settingsPath, _ := newTestEnvironment(t, "/app/searxng-admin")
	current := getTestConfig(t, handler)
	if current.Config.Brand.InstanceName != "Original" || current.Config.Search.Autocomplete != "baidu" {
		t.Fatalf("unexpected config: %+v", current.Config)
	}
	if len(current.Config.Engines) != 7 {
		t.Fatalf("engines = %d, want 7", len(current.Config.Engines))
	}
	if bing := findEngine(t, current.Config.Engines, "bing"); !bing.Enabled || !bing.DefaultEnabled || bing.Shortcut != "bi" {
		t.Fatalf("unexpected bing config: %+v", bing)
	}
	if custom := findEngine(t, current.Config.Engines, "custom engine"); custom.Origin != "custom" || !custom.Enabled {
		t.Fatalf("unexpected custom engine: %+v", custom)
	}
	chinaso := findEngine(t, current.Config.Engines, "chinaso news")
	if chinaso.Enabled || !chinaso.Locked || chinaso.Warning == "" {
		t.Fatalf("unexpected chinaso config: %+v", chinaso)
	}

	current.Config.Brand.InstanceName = "Updated"
	current.Config.Brand.BaseURL = ""
	current.Config.Brand.PrivacyPolicyURL = ""
	current.Config.Brand.DonationURL = ""
	current.Config.Brand.ContactURL = ""
	current.Config.Brand.DocsURL = ""
	current.Config.Brand.PublicInstancesURL = ""
	current.Config.Brand.WikiURL = ""
	current.Config.Brand.IssueURL = ""
	current.Config.Search.SafeSearch = 2
	current.Config.Outgoing.ProxyURL = "socks5h://127.0.0.1:9050"
	findEnginePointer(t, current.Config.Engines, "baidu").Enabled = true
	findEnginePointer(t, current.Config.Engines, "chinaso news").Enabled = true
	inactive := findEnginePointer(t, current.Config.Engines, "inactive engine")
	inactive.Enabled = true
	inactive.Inactive = false
	body, err := json.Marshal(putConfigRequest{Revision: current.Revision, Config: current.Config})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	response := performAdminRequest(handler, http.MethodPut, "/api/config", body, "application/json")
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", response.Code, response.Body.String())
	}
	var saved saveResponse
	if err := json.NewDecoder(response.Body).Decode(&saved); err != nil {
		t.Fatalf("decode save response: %v", err)
	}
	if !saved.Saved || !saved.Applied || saved.RestartRequired || saved.RolledBack || len(saved.Warnings) != 0 {
		t.Fatalf("unexpected save response: %+v", saved)
	}

	_, document, err := readYAMLDocument(settingsPath)
	if err != nil {
		t.Fatalf("read updated yaml: %v", err)
	}
	if got := scalarString(lookupPath(document, "server", "secret_key"), ""); got != "keep-me" {
		t.Fatalf("secret_key = %q", got)
	}
	if got := scalarString(lookupPath(document, "custom", "untouched"), ""); got != "preserved" {
		t.Fatalf("unknown field = %q", got)
	}
	for _, path := range [][]string{
		{"server", "base_url"},
		{"general", "privacypolicy_url"},
		{"general", "donation_url"},
		{"general", "contact_url"},
		{"brand", "docs_url"},
		{"brand", "public_instances"},
		{"brand", "wiki_url"},
		{"brand", "issue_url"},
	} {
		if node := lookupPath(document, path...); node != nil {
			t.Fatalf("empty optional setting %v should be removed, got tag=%s value=%q", path, node.Tag, node.Value)
		}
	}
	if scalarBool(mappingValue(engineNode(document, "baidu"), "disabled"), true) {
		t.Fatal("baidu should be enabled")
	}
	if !scalarBool(mappingValue(engineNode(document, "chinaso news"), "disabled"), false) {
		t.Fatal("chinaso must remain disabled")
	}
	if !scalarBool(mappingValue(engineNode(document, "chinaso news"), "inactive"), false) {
		t.Fatal("chinaso must remain inactive")
	}
	reloaded := getTestConfig(t, handler)
	if engine := findEngine(t, reloaded.Config.Engines, "inactive engine"); engine.Enabled || !engine.Inactive {
		t.Fatalf("upstream inactive engine was activated: %+v", engine)
	}
	backup, err := os.ReadFile(settingsPath + ".bak")
	if err != nil || !bytes.Contains(backup, []byte("instance_name: Original")) {
		t.Fatalf("backup missing original settings: %v", err)
	}
}

func TestPutConfigRejectsRevisionConflict(t *testing.T) {
	handler, _, _ := newTestEnvironment(t, "/app/searxng-admin")
	current := getTestConfig(t, handler)
	body, _ := json.Marshal(putConfigRequest{Revision: "stale", Config: current.Config})
	response := performAdminRequest(handler, http.MethodPut, "/api/config", body, "application/json")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
}

func TestRawConfigImportPreservesCurrentSecret(t *testing.T) {
	handler, settingsPath, _ := newTestEnvironment(t, "/app/searxng-admin")
	input := rawConfigRequest{YAML: "server:\n  secret_key: imported-secret\n  base_url: https://new.example/\ncustom:\n  imported: true\n"}
	body, _ := json.Marshal(input)
	response := performAdminRequest(handler, http.MethodPut, "/api/config/raw", body, "application/json")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	_, document, err := readYAMLDocument(settingsPath)
	if err != nil {
		t.Fatalf("read imported yaml: %v", err)
	}
	if got := scalarString(lookupPath(document, "server", "secret_key"), ""); got != "keep-me" {
		t.Fatalf("secret_key = %q", got)
	}
	if !scalarBool(lookupPath(document, "custom", "imported"), false) {
		t.Fatal("imported field not saved")
	}

	download := performAdminRequest(handler, http.MethodGet, "/api/config/raw", nil, "")
	if download.Code != http.StatusOK || !strings.Contains(download.Header().Get("Content-Disposition"), "settings.yml") {
		t.Fatalf("raw download failed: status=%d headers=%v", download.Code, download.Header())
	}
}

func TestBrandingUploadValidation(t *testing.T) {
	handler, _, brandingDir := newTestEnvironment(t, "/app/searxng-admin")
	png := makeTestPNG(t, 640, 110)
	validPNG := performAdminRequest(handler, http.MethodPost, "/api/branding/logo", png, "image/png")
	if validPNG.Code != http.StatusOK {
		t.Fatalf("PNG status = %d, body = %s", validPNG.Code, validPNG.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(brandingDir, "searxng.png")); err != nil || !bytes.Equal(data, png) {
		t.Fatalf("saved PNG mismatch: %v", err)
	}
	invalidPNG := performAdminRequest(handler, http.MethodPost, "/api/branding/favicon", []byte("not png"), "image/png")
	if invalidPNG.Code != http.StatusBadRequest {
		t.Fatalf("invalid PNG status = %d", invalidPNG.Code)
	}
	wrongPWA := performAdminRequest(handler, http.MethodPost, "/api/branding/icon192", makeTestPNG(t, 256, 256), "image/png")
	if wrongPWA.Code != http.StatusBadRequest || !strings.Contains(wrongPWA.Body.String(), "192 x 192") {
		t.Fatalf("wrong PWA dimensions status = %d, body = %s", wrongPWA.Code, wrongPWA.Body.String())
	}
	validPWA := performAdminRequest(handler, http.MethodPost, "/api/branding/icon192", makeTestPNG(t, 192, 192), "image/png")
	if validPWA.Code != http.StatusOK {
		t.Fatalf("valid PWA status = %d, body = %s", validPWA.Code, validPWA.Body.String())
	}
	validSVG := performAdminRequest(handler, http.MethodPost, "/api/branding/wordmark", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "image/svg+xml")
	if validSVG.Code != http.StatusOK {
		t.Fatalf("SVG status = %d, body = %s", validSVG.Code, validSVG.Body.String())
	}
	wrongSVG := performAdminRequest(handler, http.MethodPost, "/api/branding/wordmark", []byte(`<html></html>`), "image/svg+xml")
	if wrongSVG.Code != http.StatusBadRequest {
		t.Fatalf("invalid SVG status = %d", wrongSVG.Code)
	}
}

func makeTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := pngcodec.Encode(&output, image.NewNRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatalf("encode test PNG: %v", err)
	}
	return output.Bytes()
}

func newTestHandler(t *testing.T, prefix string) http.Handler {
	t.Helper()
	handler, _, _ := newTestEnvironment(t, prefix)
	return handler
}

func newTestEnvironment(t *testing.T, prefix string) (http.Handler, string, string) {
	t.Helper()
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>SPA</html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	configDir := t.TempDir()
	settingsPath := filepath.Join(configDir, "settings.yml")
	if err := os.WriteFile(settingsPath, []byte(testSettingsYAML()), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	brandingDir := filepath.Join(configDir, "branding")
	defaultSettingsPath := filepath.Join(configDir, "default-settings.yml")
	if err := os.WriteFile(defaultSettingsPath, []byte(testDefaultSettingsYAML()), 0o600); err != nil {
		t.Fatalf("write default settings: %v", err)
	}
	handler, err := New(Config{
		WebRoot:             webRoot,
		GatewayPrefix:       prefix,
		ServicePort:         8888,
		Version:             "test-version",
		SettingsPath:        settingsPath,
		DefaultSettingsPath: defaultSettingsPath,
		BrandingDir:         brandingDir,
		Apply: func(context.Context) (bool, error) {
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	return handler, settingsPath, brandingDir
}

func testSettingsYAML() string {
	var builder strings.Builder
	builder.WriteString("general:\n  instance_name: Original\n  privacypolicy_url: false\n  donation_url: false\n  contact_url: mailto:admin@example.com\n")
	builder.WriteString("brand:\n  docs_url: https://docs.example.com/\n  public_instances: https://instances.example.com/\n  wiki_url: https://wiki.example.com/\n  issue_url: https://issues.example.com/\n")
	builder.WriteString("search:\n  safe_search: 1\n  autocomplete: baidu\n  autocomplete_min: 4\n  favicon_resolver: duckduckgo\n  default_lang: auto\n  max_page: 0\n")
	builder.WriteString("server:\n  secret_key: keep-me\n  base_url: https://search.example.com/\n  limiter: true\n")
	builder.WriteString("ui:\n  default_theme: simple\n  default_locale: ''\n  theme_args:\n    simple_style: auto\n  query_in_title: false\n  center_alignment: false\n  results_on_new_tab: false\n  search_on_category_select: true\n  hotkeys: default\n  url_formatting: pretty\n")
	builder.WriteString("outgoing:\n  request_timeout: 3.0\n  max_request_timeout: 10.0\n  pool_connections: 100\n  pool_maxsize: 20\n  enable_http2: true\n  proxies: null\n  using_tor_proxy: false\n  extra_proxy_timeout: 0\n")
	builder.WriteString("custom:\n  untouched: preserved\nengines:\n")
	builder.WriteString("  - name: baidu\n    disabled: true\n")
	builder.WriteString("  - name: chinaso news\n    disabled: false\n    inactive: false\n")
	builder.WriteString("  - name: custom engine\n    engine: xpath\n    shortcut: ce\n    categories: general\n    disabled: false\n")
	return builder.String()
}

func testDefaultSettingsYAML() string {
	return `engines:
  - name: bing
    engine: bing
    shortcut: bi
    categories: [general]
  - name: baidu
    engine: baidu
    shortcut: bd
    categories: [general]
    disabled: true
  - name: google
    engine: google
    shortcut: go
    categories: [general]
  - name: quark
    engine: quark
    shortcut: qk
    disabled: true
  - name: inactive engine
    engine: example
    shortcut: ie
    disabled: true
    inactive: true
  - name: chinaso news
    engine: chinaso
    shortcut: chinaso
    categories: [news]
    disabled: true
    inactive: true
`
}

func getTestConfig(t *testing.T, handler http.Handler) configResponse {
	t.Helper()
	response := performAdminRequest(handler, http.MethodGet, "/api/config", nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("GET config status = %d, body = %s", response.Code, response.Body.String())
	}
	var result configResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return result
}

func performAdminRequest(handler http.Handler, method, apiPath string, body []byte, contentType string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://nas.example/app/searxng-admin"+apiPath, bytes.NewReader(body))
	request.Header.Set(adminHeader, "true")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func findEngine(t *testing.T, engines []engineConfig, name string) engineConfig {
	t.Helper()
	return *findEnginePointer(t, engines, name)
}

func findEnginePointer(t *testing.T, engines []engineConfig, name string) *engineConfig {
	t.Helper()
	for index := range engines {
		if engines[index].Name == name {
			return &engines[index]
		}
	}
	t.Fatalf("engine %q not found", name)
	return nil
}
