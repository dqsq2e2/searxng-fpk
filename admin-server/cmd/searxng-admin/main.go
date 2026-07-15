package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"searxng-admin/internal/controller"
	"searxng-admin/internal/server"
)

const version = "0.2.1"

type options struct {
	mode                string
	socketPath          string
	socketUID           int
	socketGID           int
	socketMode          os.FileMode
	webRoot             string
	gatewayPrefix       string
	servicePort         int
	settingsPath        string
	defaultSettingsPath string
	brandingDir         string
	applySocket         string
	healthURL           string
	applyTimeout        time.Duration
	dockerSocket        string
	container           string
}

func main() {
	logger := log.New(os.Stderr, "searxng-admin: ", log.LstdFlags)
	if err := run(os.Args[1:], logger); err != nil {
		logger.Printf("error: %v", err)
		os.Exit(1)
	}
}

func run(args []string, logger *log.Logger) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if options.mode == "apply-controller" {
		return runController(options, logger)
	}
	return runAdmin(options, logger)
}

func runAdmin(options options, logger *log.Logger) error {
	webRoot, err := filepath.Abs(options.webRoot)
	if err != nil {
		return fmt.Errorf("resolve web root: %w", err)
	}
	info, err := os.Stat(webRoot)
	if err != nil {
		return fmt.Errorf("inspect web root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("web root is not a directory: %s", webRoot)
	}
	handler, err := server.New(server.Config{
		WebRoot:             webRoot,
		GatewayPrefix:       options.gatewayPrefix,
		ServicePort:         options.servicePort,
		Version:             version,
		SettingsPath:        options.settingsPath,
		DefaultSettingsPath: options.defaultSettingsPath,
		BrandingDir:         options.brandingDir,
		ApplySocket:         options.applySocket,
		HealthURL:           options.healthURL,
		ApplyTimeout:        options.applyTimeout,
	})
	if err != nil {
		return err
	}
	return serveUnix(options.socketPath, handler, logger)
}

func runController(options options, logger *log.Logger) error {
	controllerServer, err := controller.New(controller.Config{
		SocketPath:   options.socketPath,
		SocketUID:    options.socketUID,
		SocketGID:    options.socketGID,
		SocketMode:   options.socketMode,
		DockerSocket: options.dockerSocket,
		Container:    options.container,
		Logger:       logger,
	})
	if err != nil {
		return err
	}
	listener, err := controllerServer.Listen()
	if err != nil {
		return err
	}
	defer controllerServer.Close()
	return serveListener(listener, controllerServer.Handler(), logger)
}

func serveUnix(socketPath string, handler http.Handler, logger *log.Logger) error {
	if err := prepareSocket(socketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket %s: %w", socketPath, err)
	}
	defer func() { _ = removeSocket(socketPath) }()
	return serveListener(listener, handler, logger)
}

func serveListener(listener net.Listener, handler http.Handler, logger *log.Logger) error {
	defer listener.Close()
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, ErrorLog: logger}
	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	serveErrors := make(chan error, 1)
	go func() {
		logger.Printf("listening on unix socket %s", listener.Addr())
		serveErrors <- httpServer.Serve(listener)
	}()
	select {
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-shutdownContext.Done():
		logger.Printf("shutdown signal received")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}
	return nil
}

func parseOptions(args []string) (options, error) {
	var result options
	var socketMode string
	flags := flag.NewFlagSet("searxng-admin", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&result.mode, "mode", "admin", "run mode: admin or apply-controller")
	flags.StringVar(&result.socketPath, "socket", "/tmp/searxng-admin.sock", "Unix socket path")
	flags.IntVar(&result.socketUID, "socket-uid", -1, "controller socket owner UID")
	flags.IntVar(&result.socketGID, "socket-gid", -1, "controller socket owner GID")
	flags.StringVar(&socketMode, "socket-mode", "0660", "controller socket mode in octal")
	flags.StringVar(&result.webRoot, "web-root", "./web", "static SPA root")
	flags.StringVar(&result.gatewayPrefix, "gateway-prefix", "/app/searxng-admin", "full gateway route prefix")
	flags.IntVar(&result.servicePort, "service-port", 8080, "SearXNG service port")
	flags.StringVar(&result.settingsPath, "settings", "/config/settings.yml", "SearXNG user settings path")
	flags.StringVar(&result.defaultSettingsPath, "default-settings", "/usr/local/searxng/searx/settings.yml", "SearXNG default settings path")
	flags.StringVar(&result.brandingDir, "branding-dir", "/config/branding", "branding asset directory")
	flags.StringVar(&result.applySocket, "apply-socket", "/data/control/apply.sock", "apply controller socket")
	flags.StringVar(&result.healthURL, "health-url", "http://searxng:8080/healthz", "SearXNG health endpoint")
	flags.DurationVar(&result.applyTimeout, "apply-timeout", 60*time.Second, "automatic apply timeout")
	flags.StringVar(&result.dockerSocket, "docker-socket", "/var/run/docker.sock", "Docker Engine Unix socket")
	flags.StringVar(&result.container, "container", "searxng-fpk", "fixed SearXNG container name")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	mode, err := strconv.ParseUint(socketMode, 8, 9)
	if err != nil {
		return options{}, fmt.Errorf("invalid socket mode: %w", err)
	}
	result.socketMode = os.FileMode(mode)
	if result.mode != "admin" && result.mode != "apply-controller" {
		return options{}, errors.New("mode must be admin or apply-controller")
	}
	if result.socketPath == "" {
		return options{}, errors.New("socket path must not be empty")
	}
	if result.mode == "admin" {
		if result.servicePort < 1 || result.servicePort > 65535 {
			return options{}, fmt.Errorf("service port must be between 1 and 65535")
		}
		if result.settingsPath == "" || result.defaultSettingsPath == "" || result.brandingDir == "" {
			return options{}, errors.New("settings, default settings, and branding paths must not be empty")
		}
		if result.applyTimeout <= 0 {
			return options{}, errors.New("apply timeout must be positive")
		}
	} else if result.dockerSocket == "" || result.container == "" {
		return options{}, errors.New("Docker socket and container must not be empty")
	}
	return result, nil
}

func prepareSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path: %s", socketPath)
	}
	return os.Remove(socketPath)
}

func removeSocket(socketPath string) error {
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
