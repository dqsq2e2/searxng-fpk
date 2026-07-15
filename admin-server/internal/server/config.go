package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxJSONBody = 2 << 20
	maxRawYAML  = 1 << 20
)

var (
	autocompleteOptions = []string{"", "360search", "baidu", "bing", "brave", "dbpedia", "duckduckgo", "google", "yandex", "privacywall", "mwmbl", "naver", "seznam", "sogou", "startpage", "swisscows", "quark", "qwant", "wikipedia"}
	faviconOptions      = []string{"", "allesedv", "duckduckgo", "google", "yandex"}
	themeOptions        = []string{"simple"}
	styleOptions        = []string{"auto", "light", "dark", "black"}
	hotkeyOptions       = []string{"default", "vim"}
	urlFormatOptions    = []string{"pretty", "full", "host"}
)

type apiConfig struct {
	Brand    brandConfig    `json:"brand"`
	Search   searchConfig   `json:"search"`
	UI       uiConfig       `json:"ui"`
	Outgoing outgoingConfig `json:"outgoing"`
	Engines  []engineConfig `json:"engines"`
}

type brandConfig struct {
	InstanceName       string `json:"instanceName"`
	BaseURL            string `json:"baseUrl"`
	PrivacyPolicyURL   string `json:"privacyPolicyUrl"`
	DonationURL        string `json:"donationUrl"`
	ContactURL         string `json:"contactUrl"`
	DocsURL            string `json:"docsUrl"`
	PublicInstancesURL string `json:"publicInstancesUrl"`
	WikiURL            string `json:"wikiUrl"`
	IssueURL           string `json:"issueUrl"`
}

type searchConfig struct {
	SafeSearch      int    `json:"safeSearch"`
	Autocomplete    string `json:"autocomplete"`
	AutocompleteMin int    `json:"autocompleteMin"`
	FaviconResolver string `json:"faviconResolver"`
	DefaultLang     string `json:"defaultLang"`
	MaxPage         int    `json:"maxPage"`
}

type uiConfig struct {
	DefaultTheme           string `json:"defaultTheme"`
	DefaultLocale          string `json:"defaultLocale"`
	SimpleStyle            string `json:"simpleStyle"`
	QueryInTitle           bool   `json:"queryInTitle"`
	CenterAlignment        bool   `json:"centerAlignment"`
	ResultsOnNewTab        bool   `json:"resultsOnNewTab"`
	SearchOnCategorySelect bool   `json:"searchOnCategorySelect"`
	Hotkeys                string `json:"hotkeys"`
	URLFormatting          string `json:"urlFormatting"`
}

type outgoingConfig struct {
	RequestTimeout    float64 `json:"requestTimeout"`
	MaxRequestTimeout float64 `json:"maxRequestTimeout"`
	PoolConnections   int     `json:"poolConnections"`
	PoolMaxSize       int     `json:"poolMaxSize"`
	EnableHTTP2       bool    `json:"enableHttp2"`
	ProxyURL          string  `json:"proxyUrl"`
	UsingTorProxy     bool    `json:"usingTorProxy"`
	ExtraProxyTimeout float64 `json:"extraProxyTimeout"`
}

type engineConfig struct {
	Name           string `json:"name"`
	Label          string `json:"label"`
	Shortcut       string `json:"shortcut"`
	Category       string `json:"category"`
	DefaultEnabled bool   `json:"defaultEnabled"`
	Enabled        bool   `json:"enabled"`
	Inactive       bool   `json:"inactive"`
	Origin         string `json:"origin"`
	Locked         bool   `json:"locked"`
	Warning        string `json:"warning"`
}

type configOptions struct {
	Autocomplete     []string `json:"autocomplete"`
	FaviconResolvers []string `json:"faviconResolvers"`
	SafeSearch       []int    `json:"safeSearch"`
	Themes           []string `json:"themes"`
	Styles           []string `json:"styles"`
	Hotkeys          []string `json:"hotkeys"`
	URLFormatting    []string `json:"urlFormatting"`
}

type configResponse struct {
	Revision string        `json:"revision"`
	Config   apiConfig     `json:"config"`
	Options  configOptions `json:"options"`
}

type putConfigRequest struct {
	Revision string    `json:"revision"`
	Config   apiConfig `json:"config"`
}

type saveResponse struct {
	Revision        string   `json:"revision"`
	Saved           bool     `json:"saved,omitempty"`
	Applied         bool     `json:"applied"`
	RestartRequired bool     `json:"restartRequired"`
	RolledBack      bool     `json:"rolledBack,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

type rawConfigRequest struct {
	YAML string `json:"yaml"`
}

func (handler *Handler) serveConfig(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handler.getConfig(response)
	case http.MethodPut:
		handler.putConfig(response, request)
	default:
		methodNotAllowed(response, http.MethodGet, http.MethodPut)
	}
}

func (handler *Handler) getConfig(response http.ResponseWriter) {
	data, document, err := readYAMLDocument(handler.settingsPath)
	if err != nil {
		writeConfigError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, configResponse{
		Revision: revision(data),
		Config:   handler.configFromYAML(document),
		Options: configOptions{
			Autocomplete:     autocompleteOptions,
			FaviconResolvers: faviconOptions,
			SafeSearch:       []int{0, 1, 2},
			Themes:           themeOptions,
			Styles:           styleOptions,
			Hotkeys:          hotkeyOptions,
			URLFormatting:    urlFormatOptions,
		},
	})
}

func (handler *Handler) putConfig(response http.ResponseWriter, request *http.Request) {
	handler.saveMu.Lock()
	defer handler.saveMu.Unlock()
	var input putConfigRequest
	if err := decodeJSON(request, &input, maxJSONBody); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := handler.validateConfig(&input.Config); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	current, document, err := readYAMLDocument(handler.settingsPath)
	if err != nil {
		writeConfigError(response, err)
		return
	}
	if input.Revision != revision(current) {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "configuration revision conflict"})
		return
	}
	handler.applyConfig(document, input.Config)
	updated, err := yaml.Marshal(document)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "encode settings.yml"})
		return
	}
	if err := atomicSaveWithBackup(handler.settingsPath, current, updated); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := handler.applySavedConfiguration(current)
	revisionData := updated
	if result.RolledBack {
		revisionData = current
	}
	writeJSON(response, http.StatusOK, saveResponse{Revision: revision(revisionData), Saved: !result.RolledBack, Applied: result.Applied, RestartRequired: result.RestartRequired, RolledBack: result.RolledBack, Warnings: result.Warnings})
}

func (handler *Handler) serveRawConfig(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		data, err := os.ReadFile(handler.settingsPath)
		if err != nil {
			writeConfigError(response, err)
			return
		}
		response.Header().Set("Content-Type", "application/x-yaml; charset=utf-8")
		response.Header().Set("Content-Disposition", `attachment; filename="settings.yml"`)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(data)
	case http.MethodPut:
		handler.putRawConfig(response, request)
	default:
		methodNotAllowed(response, http.MethodGet, http.MethodPut)
	}
}

func (handler *Handler) putRawConfig(response http.ResponseWriter, request *http.Request) {
	handler.saveMu.Lock()
	defer handler.saveMu.Unlock()
	var input rawConfigRequest
	if err := decodeJSON(request, &input, maxJSONBody); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(input.YAML) > maxRawYAML {
		writeJSON(response, http.StatusRequestEntityTooLarge, map[string]string{"error": "yaml exceeds 1 MiB"})
		return
	}
	current, currentDocument, err := readYAMLDocument(handler.settingsPath)
	if err != nil {
		writeConfigError(response, err)
		return
	}
	importedDocument, err := parseYAMLDocument([]byte(input.YAML))
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid yaml: " + err.Error()})
		return
	}
	secret := scalarString(lookupPath(currentDocument, "server", "secret_key"), "")
	if secret == "" {
		writeJSON(response, http.StatusConflict, map[string]string{"error": "current server.secret_key is missing"})
		return
	}
	setPath(importedDocument, scalarNode(secret), "server", "secret_key")
	updated, err := yaml.Marshal(importedDocument)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": "invalid yaml document"})
		return
	}
	if err := atomicSaveWithBackup(handler.settingsPath, current, updated); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := handler.applySavedConfiguration(current)
	revisionData := updated
	if result.RolledBack {
		revisionData = current
	}
	writeJSON(response, http.StatusOK, saveResponse{Revision: revision(revisionData), Saved: !result.RolledBack, Applied: result.Applied, RestartRequired: result.RestartRequired, RolledBack: result.RolledBack, Warnings: result.Warnings})
}

func (handler *Handler) configFromYAML(document *yaml.Node) apiConfig {
	config := apiConfig{
		Brand: brandConfig{
			InstanceName:       scalarString(lookupPath(document, "general", "instance_name"), "SearXNG"),
			BaseURL:            scalarString(lookupPath(document, "server", "base_url"), ""),
			PrivacyPolicyURL:   scalarString(lookupPath(document, "general", "privacypolicy_url"), ""),
			DonationURL:        scalarString(lookupPath(document, "general", "donation_url"), ""),
			ContactURL:         scalarString(lookupPath(document, "general", "contact_url"), ""),
			DocsURL:            scalarString(lookupPath(document, "brand", "docs_url"), ""),
			PublicInstancesURL: scalarString(lookupPath(document, "brand", "public_instances"), ""),
			WikiURL:            scalarString(lookupPath(document, "brand", "wiki_url"), ""),
			IssueURL:           scalarString(lookupPath(document, "brand", "issue_url"), ""),
		},
		Search: searchConfig{
			SafeSearch:      scalarInt(lookupPath(document, "search", "safe_search"), 0),
			Autocomplete:    scalarString(lookupPath(document, "search", "autocomplete"), ""),
			AutocompleteMin: scalarInt(lookupPath(document, "search", "autocomplete_min"), 4),
			FaviconResolver: scalarString(lookupPath(document, "search", "favicon_resolver"), ""),
			DefaultLang:     scalarString(lookupPath(document, "search", "default_lang"), "auto"),
			MaxPage:         scalarInt(lookupPath(document, "search", "max_page"), 0),
		},
		UI: uiConfig{
			DefaultTheme:           scalarString(lookupPath(document, "ui", "default_theme"), "simple"),
			DefaultLocale:          scalarString(lookupPath(document, "ui", "default_locale"), ""),
			SimpleStyle:            scalarString(lookupPath(document, "ui", "theme_args", "simple_style"), "auto"),
			QueryInTitle:           scalarBool(lookupPath(document, "ui", "query_in_title"), false),
			CenterAlignment:        scalarBool(lookupPath(document, "ui", "center_alignment"), false),
			ResultsOnNewTab:        scalarBool(lookupPath(document, "ui", "results_on_new_tab"), false),
			SearchOnCategorySelect: scalarBool(lookupPath(document, "ui", "search_on_category_select"), true),
			Hotkeys:                scalarString(lookupPath(document, "ui", "hotkeys"), "default"),
			URLFormatting:          scalarString(lookupPath(document, "ui", "url_formatting"), "pretty"),
		},
		Outgoing: outgoingConfig{
			RequestTimeout:    scalarFloat(lookupPath(document, "outgoing", "request_timeout"), 3),
			MaxRequestTimeout: scalarFloat(lookupPath(document, "outgoing", "max_request_timeout"), 0),
			PoolConnections:   scalarInt(lookupPath(document, "outgoing", "pool_connections"), 100),
			PoolMaxSize:       scalarInt(lookupPath(document, "outgoing", "pool_maxsize"), 20),
			EnableHTTP2:       scalarBool(lookupPath(document, "outgoing", "enable_http2"), true),
			ProxyURL:          scalarString(lookupPath(document, "outgoing", "proxies"), ""),
			UsingTorProxy:     scalarBool(lookupPath(document, "outgoing", "using_tor_proxy"), false),
			ExtraProxyTimeout: scalarFloat(lookupPath(document, "outgoing", "extra_proxy_timeout"), 0),
		},
	}
	config.Engines = handler.enginesFromYAML(document)
	return config
}

func (handler *Handler) validateConfig(config *apiConfig) error {
	if strings.TrimSpace(config.Brand.InstanceName) == "" || len(config.Brand.InstanceName) > 100 {
		return errors.New("brand.instanceName must be 1-100 characters")
	}
	urlFields := []struct {
		name        string
		value       string
		allowMailto bool
	}{
		{"brand.baseUrl", config.Brand.BaseURL, false},
		{"brand.privacyPolicyUrl", config.Brand.PrivacyPolicyURL, false},
		{"brand.donationUrl", config.Brand.DonationURL, false},
		{"brand.contactUrl", config.Brand.ContactURL, true},
		{"brand.docsUrl", config.Brand.DocsURL, false},
		{"brand.publicInstancesUrl", config.Brand.PublicInstancesURL, false},
		{"brand.wikiUrl", config.Brand.WikiURL, false},
		{"brand.issueUrl", config.Brand.IssueURL, false},
	}
	for _, field := range urlFields {
		if err := validateURL(field.name, field.value, field.allowMailto); err != nil {
			return err
		}
	}
	if !slices.Contains([]int{0, 1, 2}, config.Search.SafeSearch) {
		return errors.New("search.safeSearch must be 0, 1, or 2")
	}
	if !slices.Contains(autocompleteOptions, config.Search.Autocomplete) {
		return errors.New("search.autocomplete is invalid")
	}
	if config.Search.AutocompleteMin < 1 || config.Search.AutocompleteMin > 100 {
		return errors.New("search.autocompleteMin must be between 1 and 100")
	}
	if !slices.Contains(faviconOptions, config.Search.FaviconResolver) {
		return errors.New("search.faviconResolver is invalid")
	}
	if config.Search.DefaultLang == "" || len(config.Search.DefaultLang) > 35 {
		return errors.New("search.defaultLang is invalid")
	}
	if config.Search.MaxPage < 0 || config.Search.MaxPage > 10000 {
		return errors.New("search.maxPage must be between 0 and 10000")
	}
	if !slices.Contains(themeOptions, config.UI.DefaultTheme) {
		return errors.New("ui.defaultTheme is invalid")
	}
	if len(config.UI.DefaultLocale) > 35 {
		return errors.New("ui.defaultLocale is invalid")
	}
	if !slices.Contains(styleOptions, config.UI.SimpleStyle) {
		return errors.New("ui.simpleStyle is invalid")
	}
	if !slices.Contains(hotkeyOptions, config.UI.Hotkeys) {
		return errors.New("ui.hotkeys is invalid")
	}
	if !slices.Contains(urlFormatOptions, config.UI.URLFormatting) {
		return errors.New("ui.urlFormatting is invalid")
	}
	if config.Outgoing.RequestTimeout <= 0 || config.Outgoing.RequestTimeout > 300 {
		return errors.New("outgoing.requestTimeout must be greater than 0 and at most 300")
	}
	if config.Outgoing.MaxRequestTimeout < 0 || config.Outgoing.MaxRequestTimeout > 600 {
		return errors.New("outgoing.maxRequestTimeout must be between 0 and 600")
	}
	if config.Outgoing.MaxRequestTimeout > 0 && config.Outgoing.MaxRequestTimeout < config.Outgoing.RequestTimeout {
		return errors.New("outgoing.maxRequestTimeout must not be less than requestTimeout")
	}
	if config.Outgoing.PoolConnections < 1 || config.Outgoing.PoolConnections > 10000 {
		return errors.New("outgoing.poolConnections must be between 1 and 10000")
	}
	if config.Outgoing.PoolMaxSize < 1 || config.Outgoing.PoolMaxSize > 10000 {
		return errors.New("outgoing.poolMaxSize must be between 1 and 10000")
	}
	if config.Outgoing.ExtraProxyTimeout < 0 || config.Outgoing.ExtraProxyTimeout > 600 {
		return errors.New("outgoing.extraProxyTimeout must be between 0 and 600")
	}
	if err := validateProxy(config.Outgoing.ProxyURL); err != nil {
		return err
	}
	knownEngines := make(map[string]engineDefinition, len(handler.engineCatalog))
	for _, definition := range handler.engineCatalog {
		knownEngines[definition.Name] = definition
	}
	customEngines := customEngineNames(handler.settingsPath, knownEngines)
	if len(config.Engines) != len(handler.engineCatalog)+len(customEngines) {
		return errors.New("engines must contain the complete engine list")
	}
	seen := make(map[string]bool, len(config.Engines))
	for index := range config.Engines {
		engine := &config.Engines[index]
		definition, known := knownEngines[engine.Name]
		_, custom := customEngines[engine.Name]
		if (!known && !custom) || seen[engine.Name] {
			return fmt.Errorf("invalid or duplicate engine %q", engine.Name)
		}
		seen[engine.Name] = true
		if engine.Name == "chinaso news" {
			engine.Enabled = false
			engine.Inactive = true
		} else if known && definition.DefaultInactive {
			engine.Enabled = false
			engine.Inactive = true
		} else if engine.Inactive {
			engine.Enabled = false
		}
	}
	return nil
}

func (handler *Handler) applyConfig(document *yaml.Node, config apiConfig) {
	setPath(document, scalarNode(config.Brand.InstanceName), "general", "instance_name")
	setOptionalString(document, config.Brand.BaseURL, "server", "base_url")
	setOptionalString(document, config.Brand.PrivacyPolicyURL, "general", "privacypolicy_url")
	setOptionalString(document, config.Brand.DonationURL, "general", "donation_url")
	setOptionalString(document, config.Brand.ContactURL, "general", "contact_url")
	setOptionalString(document, config.Brand.DocsURL, "brand", "docs_url")
	setOptionalString(document, config.Brand.PublicInstancesURL, "brand", "public_instances")
	setOptionalString(document, config.Brand.WikiURL, "brand", "wiki_url")
	setOptionalString(document, config.Brand.IssueURL, "brand", "issue_url")
	setPath(document, scalarNode(config.Search.SafeSearch), "search", "safe_search")
	setPath(document, scalarNode(config.Search.Autocomplete), "search", "autocomplete")
	setPath(document, scalarNode(config.Search.AutocompleteMin), "search", "autocomplete_min")
	setPath(document, scalarNode(config.Search.FaviconResolver), "search", "favicon_resolver")
	setPath(document, scalarNode(config.Search.DefaultLang), "search", "default_lang")
	setPath(document, scalarNode(config.Search.MaxPage), "search", "max_page")
	setPath(document, scalarNode(config.UI.DefaultTheme), "ui", "default_theme")
	setPath(document, scalarNode(config.UI.DefaultLocale), "ui", "default_locale")
	setPath(document, scalarNode(config.UI.SimpleStyle), "ui", "theme_args", "simple_style")
	setPath(document, scalarNode(config.UI.QueryInTitle), "ui", "query_in_title")
	setPath(document, scalarNode(config.UI.CenterAlignment), "ui", "center_alignment")
	setPath(document, scalarNode(config.UI.ResultsOnNewTab), "ui", "results_on_new_tab")
	setPath(document, scalarNode(config.UI.SearchOnCategorySelect), "ui", "search_on_category_select")
	setPath(document, scalarNode(config.UI.Hotkeys), "ui", "hotkeys")
	setPath(document, scalarNode(config.UI.URLFormatting), "ui", "url_formatting")
	setPath(document, scalarNode(config.Outgoing.RequestTimeout), "outgoing", "request_timeout")
	if config.Outgoing.MaxRequestTimeout == 0 {
		setPath(document, nullNode(), "outgoing", "max_request_timeout")
	} else {
		setPath(document, scalarNode(config.Outgoing.MaxRequestTimeout), "outgoing", "max_request_timeout")
	}
	setPath(document, scalarNode(config.Outgoing.PoolConnections), "outgoing", "pool_connections")
	setPath(document, scalarNode(config.Outgoing.PoolMaxSize), "outgoing", "pool_maxsize")
	setPath(document, scalarNode(config.Outgoing.EnableHTTP2), "outgoing", "enable_http2")
	if config.Outgoing.ProxyURL == "" {
		setPath(document, nullNode(), "outgoing", "proxies")
	} else {
		setPath(document, scalarNode(config.Outgoing.ProxyURL), "outgoing", "proxies")
	}
	setPath(document, scalarNode(config.Outgoing.UsingTorProxy), "outgoing", "using_tor_proxy")
	setPath(document, scalarNode(config.Outgoing.ExtraProxyTimeout), "outgoing", "extra_proxy_timeout")
	handler.applyEngines(document, config.Engines)
}

func readYAMLDocument(filename string) ([]byte, *yaml.Node, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, nil, err
	}
	document, err := parseYAMLDocument(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parse settings.yml: %w", err)
	}
	return data, document, nil
}

func parseYAMLDocument(data []byte) (*yaml.Node, error) {
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("root must be a mapping")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple YAML documents are not allowed")
		}
		return nil, err
	}
	return &document, nil
}

func decodeJSON(request *http.Request, target any, limit int64) error {
	reader := io.LimitReader(request.Body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	if int64(len(data)) > limit {
		return errors.New("request body too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request must contain one JSON object")
	}
	return nil
}

func validateURL(name, value string, allowMailto bool) error {
	if value == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("%s must be an absolute URL", name)
	}
	allowed := parsed.Scheme == "http" || parsed.Scheme == "https" || (allowMailto && parsed.Scheme == "mailto")
	if !allowed || (parsed.Scheme != "mailto" && parsed.Host == "") {
		return fmt.Errorf("%s uses an unsupported URL scheme", name)
	}
	return nil
}

func validateProxy(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("outgoing.proxyUrl must be an absolute proxy URL")
	}
	if !slices.Contains([]string{"http", "https", "socks4", "socks5", "socks5h"}, strings.ToLower(parsed.Scheme)) {
		return errors.New("outgoing.proxyUrl uses an unsupported proxy protocol")
	}
	return nil
}

func revision(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func atomicSaveWithBackup(filename string, current, updated []byte) error {
	info, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("inspect settings.yml: %w", err)
	}
	if err := atomicWrite(filename+".bak", current, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write settings.yml.bak: %w", err)
	}
	if err := atomicWrite(filename, updated, info.Mode().Perm()); err != nil {
		return fmt.Errorf("write settings.yml: %w", err)
	}
	return nil
}

func atomicWrite(filename string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(filename)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".searxng-admin-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, filename)
}

func writeConfigError(response http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, os.ErrNotExist) {
		status = http.StatusNotFound
	}
	writeJSON(response, status, map[string]string{"error": err.Error()})
}

func lookupPath(document *yaml.Node, keys ...string) *yaml.Node {
	if document == nil || len(document.Content) == 0 {
		return nil
	}
	current := document.Content[0]
	for _, key := range keys {
		current = mappingValue(current, key)
		if current == nil {
			return nil
		}
	}
	return current
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func setPath(document *yaml.Node, value *yaml.Node, keys ...string) {
	current := document.Content[0]
	for _, key := range keys[:len(keys)-1] {
		next := mappingValue(current, key)
		if next == nil || next.Kind != yaml.MappingNode {
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			setMappingValue(current, key, next)
		}
		current = next
	}
	setMappingValue(current, keys[len(keys)-1], value)
}

func setOptionalString(document *yaml.Node, value string, keys ...string) {
	if value == "" {
		deletePath(document, keys...)
		return
	}
	setPath(document, scalarNode(value), keys...)
}

func deletePath(document *yaml.Node, keys ...string) {
	if document == nil || len(document.Content) == 0 || len(keys) == 0 {
		return
	}
	current := document.Content[0]
	for _, key := range keys[:len(keys)-1] {
		current = mappingValue(current, key)
		if current == nil || current.Kind != yaml.MappingNode {
			return
		}
	}
	key := keys[len(keys)-1]
	for index := 0; index+1 < len(current.Content); index += 2 {
		if current.Content[index].Value == key {
			current.Content = append(current.Content[:index], current.Content[index+2:]...)
			return
		}
	}
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func scalarNode(value any) *yaml.Node {
	var node yaml.Node
	_ = node.Encode(value)
	return &node
}

func nullNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
}

func scalarString(node *yaml.Node, fallback string) string {
	if node == nil || node.Tag == "!!null" || (node.Tag == "!!bool" && node.Value == "false") {
		return fallback
	}
	var value string
	if err := node.Decode(&value); err != nil {
		return fallback
	}
	return value
}

func scalarInt(node *yaml.Node, fallback int) int {
	if node == nil {
		return fallback
	}
	var value int
	if node.Decode(&value) != nil {
		return fallback
	}
	return value
}

func scalarFloat(node *yaml.Node, fallback float64) float64 {
	if node == nil || node.Tag == "!!null" {
		return fallback
	}
	var value float64
	if node.Decode(&value) != nil {
		return fallback
	}
	return value
}

func scalarBool(node *yaml.Node, fallback bool) bool {
	if node == nil {
		return fallback
	}
	var value bool
	if node.Decode(&value) != nil {
		return fallback
	}
	return value
}

func engineNode(document *yaml.Node, name string) *yaml.Node {
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		return nil
	}
	for _, engine := range engines.Content {
		if scalarString(mappingValue(engine, "name"), "") == name {
			return engine
		}
	}
	return nil
}

func (handler *Handler) enginesFromYAML(document *yaml.Node) []engineConfig {
	engines := make([]engineConfig, 0, len(handler.engineCatalog))
	known := make(map[string]struct{}, len(handler.engineCatalog))
	for _, definition := range handler.engineCatalog {
		known[definition.Name] = struct{}{}
		override := engineNode(document, definition.Name)
		disabled := nodeBool(mappingValue(override, "disabled"), definition.DefaultDisabled)
		inactive := nodeBool(mappingValue(override, "inactive"), definition.DefaultInactive)
		locked := definition.Name == "chinaso news"
		warning := ""
		if locked {
			disabled = true
			inactive = true
			warning = "该引擎的外部链接可能泄露用户搜索信息，存在隐私风险，因此已强制禁用。"
		} else if inactive {
			warning = "该引擎被 SearXNG 上游标记为 inactive，当前版本不可用。"
		}
		engines = append(engines, engineConfig{
			Name: definition.Name, Label: definition.Label, Shortcut: definition.Shortcut, Category: definition.Category,
			DefaultEnabled: !definition.DefaultDisabled && !definition.DefaultInactive,
			Enabled:        !disabled && !inactive, Inactive: inactive, Origin: "default", Locked: locked, Warning: warning,
		})
	}
	configured := lookupPath(document, "engines")
	if configured != nil && configured.Kind == yaml.SequenceNode {
		for _, engine := range configured.Content {
			name := scalarString(mappingValue(engine, "name"), "")
			if name == "" {
				continue
			}
			if _, exists := known[name]; exists {
				continue
			}
			disabled := scalarBool(mappingValue(engine, "disabled"), false)
			inactive := scalarBool(mappingValue(engine, "inactive"), false)
			engines = append(engines, engineConfig{
				Name: name, Label: name, Shortcut: scalarString(mappingValue(engine, "shortcut"), ""), Category: engineCategory(engine),
				DefaultEnabled: true, Enabled: !disabled && !inactive, Inactive: inactive, Origin: "custom",
			})
		}
	}
	return engines
}

func nodeBool(node *yaml.Node, fallback bool) bool {
	if node == nil {
		return fallback
	}
	return scalarBool(node, fallback)
}

func customEngineNames(settingsPath string, known map[string]engineDefinition) map[string]struct{} {
	result := make(map[string]struct{})
	_, document, err := readYAMLDocument(settingsPath)
	if err != nil {
		return result
	}
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		return result
	}
	for _, engine := range engines.Content {
		name := scalarString(mappingValue(engine, "name"), "")
		if _, exists := known[name]; name != "" && !exists {
			result[name] = struct{}{}
		}
	}
	return result
}

func (handler *Handler) applyEngines(document *yaml.Node, requested []engineConfig) {
	definitions := make(map[string]engineDefinition, len(handler.engineCatalog))
	for _, definition := range handler.engineCatalog {
		definitions[definition.Name] = definition
	}
	for _, engine := range requested {
		definition, isDefault := definitions[engine.Name]
		disabled := !engine.Enabled
		inactive := engine.Inactive
		if engine.Enabled {
			inactive = false
		}
		if engine.Name == "chinaso news" {
			node := ensureEngineNode(document, engine.Name)
			setMappingValue(node, "disabled", scalarNode(true))
			setMappingValue(node, "inactive", scalarNode(true))
			continue
		}
		if isDefault && definition.DefaultInactive {
			disabled = definition.DefaultDisabled
			inactive = true
		}
		if isDefault {
			setEngineOverride(document, engine.Name, "disabled", disabled, definition.DefaultDisabled)
			setEngineOverride(document, engine.Name, "inactive", inactive, definition.DefaultInactive)
			removeEmptyDefaultEngine(document, engine.Name)
			continue
		}
		node := ensureEngineNode(document, engine.Name)
		setMappingValue(node, "disabled", scalarNode(disabled))
		setMappingValue(node, "inactive", scalarNode(inactive))
	}
}

func setEngineOverride(document *yaml.Node, name, key string, value, defaultValue bool) {
	node := engineNode(document, name)
	if value == defaultValue {
		if node != nil {
			deleteMappingValue(node, key)
		}
		return
	}
	node = ensureEngineNode(document, name)
	setMappingValue(node, key, scalarNode(value))
}

func ensureEngineNode(document *yaml.Node, name string) *yaml.Node {
	if existing := engineNode(document, name); existing != nil {
		return existing
	}
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		engines = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setPath(document, engines, "engines")
	}
	engine := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingValue(engine, "name", scalarNode(name))
	engines.Content = append(engines.Content, engine)
	return engine
}

func removeEmptyDefaultEngine(document *yaml.Node, name string) {
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		return
	}
	for index, engine := range engines.Content {
		if scalarString(mappingValue(engine, "name"), "") == name && len(engine.Content) == 2 {
			engines.Content = append(engines.Content[:index], engines.Content[index+1:]...)
			return
		}
	}
}

func deleteMappingValue(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return
		}
	}
}
