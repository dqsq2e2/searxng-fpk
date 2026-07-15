package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func newTestHandler(t *testing.T, prefix string) http.Handler {
	t.Helper()
	webRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>SPA</html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	handler, err := New(Config{
		WebRoot:       webRoot,
		GatewayPrefix: prefix,
		ServicePort:   8888,
		Version:       "test-version",
	})
	if err != nil {
		t.Fatalf("create handler: %v", err)
	}
	return handler
}
