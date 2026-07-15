package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

type applicationResult struct {
	Applied         bool
	RestartRequired bool
	RolledBack      bool
	Warnings        []string
}

func newApplyClient(socketPath string) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		if socketPath == "" {
			return false, errors.New("apply controller socket is not configured")
		}
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/apply", bytes.NewReader(nil))
		if err != nil {
			return false, err
		}
		response, err := client.Do(request)
		if err != nil {
			return false, err
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return false, fmt.Errorf("apply controller returned %s", response.Status)
		}
		return true, nil
	}
}

func (handler *Handler) applySavedConfiguration(previous []byte) applicationResult {
	return handler.applyChange(func() error { return atomicWrite(handler.settingsPath, previous, 0o600) })
}

func (handler *Handler) applyBrandingAsset(filename string, previous []byte, existed bool) applicationResult {
	return handler.applyChange(func() error {
		if existed {
			return atomicWrite(filename, previous, 0o644)
		}
		return os.Remove(filename)
	})
}

func (handler *Handler) applyChange(rollback func() error) applicationResult {
	ctx, cancel := context.WithTimeout(context.Background(), handler.applyTimeout)
	defer cancel()
	executed, err := handler.apply(ctx)
	if err != nil || !executed {
		warning := "配置已保存，但自动应用服务不可用，请手动重启 SearXNG"
		if err != nil {
			warning += "：" + err.Error()
		}
		return applicationResult{RestartRequired: true, Warnings: []string{warning}}
	}
	if err := handler.waitHealthy(ctx); err == nil {
		return applicationResult{Applied: true}
	}

	rollbackWarning := "新配置应用后服务未恢复健康，已自动回滚"
	if err := rollback(); err != nil {
		return applicationResult{RestartRequired: true, Warnings: []string{rollbackWarning + "失败：" + err.Error()}}
	}
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), handler.applyTimeout)
	defer rollbackCancel()
	rollbackExecuted, rollbackErr := handler.apply(rollbackCtx)
	if rollbackErr != nil || !rollbackExecuted {
		if rollbackErr != nil {
			rollbackWarning += "，但回滚配置无法自动重启：" + rollbackErr.Error()
		}
		return applicationResult{RestartRequired: true, RolledBack: true, Warnings: []string{rollbackWarning}}
	}
	if err := handler.waitHealthy(rollbackCtx); err != nil {
		return applicationResult{RestartRequired: true, RolledBack: true, Warnings: []string{rollbackWarning + "，且原配置重启后仍不健康：" + err.Error()}}
	}
	return applicationResult{RolledBack: true, Warnings: []string{rollbackWarning}}
}

func (handler *Handler) waitHealthy(ctx context.Context) error {
	if handler.healthURL == "" {
		return nil
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastError error
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, handler.healthURL, nil)
		if err != nil {
			return err
		}
		response, err := handler.healthClient.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				return nil
			}
			lastError = fmt.Errorf("health endpoint returned %s", response.Status)
		} else {
			lastError = err
		}
		select {
		case <-ctx.Done():
			if lastError == nil {
				lastError = ctx.Err()
			}
			return lastError
		case <-ticker.C:
		}
	}
}
