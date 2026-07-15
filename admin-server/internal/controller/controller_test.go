package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestApplyRestartsOnlyConfiguredContainer(t *testing.T) {
	server, err := New(Config{SocketPath: "/tmp/apply.sock", DockerSocket: "/tmp/docker.sock", Container: "searxng-fpk"})
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	server.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		if request.URL.Path != "/containers/searxng-fpk/restart" || request.URL.Query().Get("t") != "30" {
			t.Fatalf("unexpected Docker endpoint: %s", request.URL.String())
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	})}

	request := httptest.NewRequest(http.MethodPost, "http://unix/apply", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestApplyRejectsOtherMethodsAndPaths(t *testing.T) {
	server, err := New(Config{SocketPath: "/tmp/apply.sock", DockerSocket: "/tmp/docker.sock", Container: "searxng-fpk"})
	if err != nil {
		t.Fatalf("create controller: %v", err)
	}
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/apply", http.StatusMethodNotAllowed},
		{http.MethodPost, "/containers/other/restart", http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(test.method, "http://unix"+test.path, nil))
		if response.Code != test.status {
			t.Fatalf("%s %s status = %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}
