package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const adminHeader = "X-Trim-Isadmin"

type Config struct {
	WebRoot       string
	GatewayPrefix string
	ServicePort   int
	Version       string
}

type Handler struct {
	webRoot       string
	gatewayPrefix string
	servicePort   int
	version       string
}

type statusResponse struct {
	Status     string `json:"status"`
	Version    string `json:"version"`
	ServiceURL string `json:"serviceUrl"`
}

func New(config Config) (http.Handler, error) {
	prefix, err := normalizePrefix(config.GatewayPrefix)
	if err != nil {
		return nil, err
	}
	if config.ServicePort < 1 || config.ServicePort > 65535 {
		return nil, fmt.Errorf("service port must be between 1 and 65535")
	}
	root, err := filepath.Abs(config.WebRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve web root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve web root symlinks: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect web root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("web root is not a directory: %s", root)
	}

	return &Handler{
		webRoot:       root,
		gatewayPrefix: prefix,
		servicePort:   config.ServicePort,
		version:       config.Version,
	}, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path == handler.gatewayPrefix {
		http.Redirect(response, request, handler.gatewayPrefix+"/", http.StatusTemporaryRedirect)
		return
	}
	if !strings.HasPrefix(request.URL.Path, handler.gatewayPrefix+"/") {
		http.NotFound(response, request)
		return
	}

	relativePath := strings.TrimPrefix(request.URL.Path, handler.gatewayPrefix)
	if relativePath == "/api" || strings.HasPrefix(relativePath, "/api/") {
		handler.serveAPI(response, request, relativePath)
		return
	}
	handler.serveSPA(response, request, relativePath)
}

func (handler *Handler) serveAPI(response http.ResponseWriter, request *http.Request, relativePath string) {
	if request.Header.Get(adminHeader) != "true" {
		writeJSON(response, http.StatusForbidden, map[string]string{"error": "administrator access required"})
		return
	}
	if relativePath != "/api/status" {
		http.NotFound(response, request)
		return
	}
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeJSON(response, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	writeJSON(response, http.StatusOK, statusResponse{
		Status:     "ok",
		Version:    handler.version,
		ServiceURL: serviceURL(request, handler.servicePort),
	})
}

func (handler *Handler) serveSPA(response http.ResponseWriter, request *http.Request, relativePath string) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requested, err := handler.resolveStaticPath(relativePath)
	if err != nil {
		http.Error(response, "invalid path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(requested)
	if err == nil && info.Mode().IsRegular() {
		serveFile(response, request, requested, info)
		return
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		http.Error(response, "unable to read static file", http.StatusInternalServerError)
		return
	}

	indexPath, err := handler.resolveStaticPath("/index.html")
	if err != nil {
		http.Error(response, "invalid web root", http.StatusInternalServerError)
		return
	}
	indexInfo, err := os.Stat(indexPath)
	if err != nil || !indexInfo.Mode().IsRegular() {
		http.Error(response, "SPA index not found", http.StatusNotFound)
		return
	}
	serveFile(response, request, indexPath, indexInfo)
}

func (handler *Handler) resolveStaticPath(requestPath string) (string, error) {
	if strings.ContainsRune(requestPath, '\x00') || strings.Contains(requestPath, "\\") {
		return "", errors.New("invalid path character")
	}
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == ".." {
			return "", errors.New("path traversal")
		}
	}

	cleanPath := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	candidate := filepath.Join(handler.webRoot, filepath.FromSlash(cleanPath))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
		return "", err
	}
	if !withinRoot(handler.webRoot, resolved) {
		return "", errors.New("path escapes web root")
	}
	return resolved, nil
}

func withinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func serveFile(response http.ResponseWriter, request *http.Request, filename string, info os.FileInfo) {
	if contentType := mime.TypeByExtension(filepath.Ext(filename)); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	file, err := os.Open(filename)
	if err != nil {
		http.Error(response, "unable to open static file", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	http.ServeContent(response, request, info.Name(), info.ModTime(), file)
}

func serviceURL(request *http.Request, port int) string {
	scheme := "http"
	if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	} else if request.TLS != nil {
		scheme = "https"
	}

	host := request.Host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	} else {
		host = strings.Trim(host, "[]")
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func normalizePrefix(prefix string) (string, error) {
	if prefix == "" || !strings.HasPrefix(prefix, "/") {
		return "", errors.New("gateway prefix must be an absolute URL path")
	}
	if strings.Contains(prefix, "\\") || strings.ContainsRune(prefix, '\x00') {
		return "", errors.New("gateway prefix contains invalid characters")
	}
	cleaned := path.Clean(prefix)
	if cleaned == "/" {
		return "", errors.New("gateway prefix must not be root")
	}
	if cleaned != strings.TrimSuffix(prefix, "/") {
		return "", errors.New("gateway prefix must be normalized")
	}
	return cleaned, nil
}
