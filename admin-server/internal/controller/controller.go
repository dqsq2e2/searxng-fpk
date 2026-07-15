package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	SocketPath   string
	SocketUID    int
	SocketGID    int
	SocketMode   os.FileMode
	DockerSocket string
	Container    string
	Logger       *log.Logger
}

type Server struct {
	config Config
	client *http.Client
}

func New(config Config) (*Server, error) {
	if config.SocketPath == "" || config.DockerSocket == "" || config.Container == "" {
		return nil, errors.New("controller socket, Docker socket, and container are required")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", config.DockerSocket)
		},
	}
	return &Server{config: config, client: &http.Client{Transport: transport, Timeout: 45 * time.Second}}, nil
}

func (server *Server) Listen() (net.Listener, error) {
	if err := removeStaleSocket(server.config.SocketPath); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", server.config.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on controller socket: %w", err)
	}
	if err := os.Chmod(server.config.SocketPath, server.config.SocketMode); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod controller socket: %w", err)
	}
	if server.config.SocketUID >= 0 || server.config.SocketGID >= 0 {
		if err := os.Chown(server.config.SocketPath, server.config.SocketUID, server.config.SocketGID); err != nil {
			listener.Close()
			return nil, fmt.Errorf("chown controller socket: %w", err)
		}
	}
	return listener, nil
}

func (server *Server) Handler() http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/apply" {
			http.NotFound(response, request)
			return
		}
		if request.Method != http.MethodPost {
			response.Header().Set("Allow", http.MethodPost)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if request.ContentLength > 0 {
			http.Error(response, "request body is not allowed", http.StatusBadRequest)
			return
		}
		if err := server.restart(request.Context()); err != nil {
			server.logf("restart failed: %v", err)
			writeJSON(response, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(response, http.StatusOK, map[string]bool{"restarted": true})
	})
}

func (server *Server) Close() {
	server.client.CloseIdleConnections()
	_ = os.Remove(server.config.SocketPath)
}

func (server *Server) restart(ctx context.Context) error {
	endpoint := "http://docker/containers/" + url.PathEscape(server.config.Container) + "/restart?t=" + strconv.Itoa(30)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := server.client.Do(request)
	if err != nil {
		return fmt.Errorf("call Docker restart API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Docker restart API returned %s", response.Status)
	}
	return nil
}

func (server *Server) logf(format string, args ...any) {
	if server.config.Logger != nil {
		server.config.Logger.Printf(format, args...)
	}
}

func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path: %s", socketPath)
	}
	return os.Remove(socketPath)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
