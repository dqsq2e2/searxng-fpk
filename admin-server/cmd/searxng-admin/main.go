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
	"syscall"
	"time"

	"searxng-admin/internal/server"
)

const version = "0.1.0"

type options struct {
	socketPath    string
	webRoot       string
	gatewayPrefix string
	servicePort   int
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

	if err := prepareSocket(options.socketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", options.socketPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket %s: %w", options.socketPath, err)
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Printf("close listener: %v", err)
		}
		if err := removeSocket(options.socketPath); err != nil {
			logger.Printf("remove socket: %v", err)
		}
	}()

	handler, err := server.New(server.Config{
		WebRoot:       webRoot,
		GatewayPrefix: options.gatewayPrefix,
		ServicePort:   options.servicePort,
		Version:       version,
	})
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          logger,
	}

	shutdownContext, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErrors := make(chan error, 1)
	go func() {
		logger.Printf("listening on unix socket %s", options.socketPath)
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
	flags := flag.NewFlagSet("searxng-admin", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&result.socketPath, "socket", "/tmp/searxng-admin.sock", "Unix socket path")
	flags.StringVar(&result.webRoot, "web-root", "./web", "static SPA root")
	flags.StringVar(&result.gatewayPrefix, "gateway-prefix", "/app/searxng-admin", "full gateway route prefix")
	flags.IntVar(&result.servicePort, "service-port", 8080, "SearXNG service port")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if result.socketPath == "" {
		return options{}, errors.New("socket path must not be empty")
	}
	if result.servicePort < 1 || result.servicePort > 65535 {
		return options{}, fmt.Errorf("service port must be between 1 and 65535")
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
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
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
