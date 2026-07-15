package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type healthRoundTripFunc func(*http.Request) (*http.Response, error)

func (function healthRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestApplyUnavailableKeepsSavedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{
		settingsPath: path,
		applyTimeout: time.Second,
		apply: func(context.Context) (bool, error) {
			return false, errors.New("controller unavailable")
		},
	}
	result := handler.applySavedConfiguration([]byte("old"))
	if result.Applied || result.RolledBack || !result.RestartRequired || len(result.Warnings) == 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, []byte("new")) {
		t.Fatalf("saved configuration was not retained: %q", data)
	}
}

func TestUnhealthyApplicationRollsBackAndRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yml")
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	applyCalls := 0
	handler := &Handler{
		settingsPath: path,
		applyTimeout: 20 * time.Millisecond,
		healthURL:    "http://searxng/healthz",
		healthClient: &http.Client{Transport: healthRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(bytes.NewReader(nil))}, nil
		})},
		apply: func(context.Context) (bool, error) {
			applyCalls++
			return true, nil
		},
	}
	result := handler.applySavedConfiguration([]byte("old"))
	if !result.RolledBack || result.Applied || !result.RestartRequired || applyCalls != 2 {
		t.Fatalf("unexpected result: %+v, apply calls=%d", result, applyCalls)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, []byte("old")) {
		t.Fatalf("configuration was not rolled back: %q", data)
	}
}
