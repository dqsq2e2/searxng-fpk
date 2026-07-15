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
	Name     string `json:"name"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Enabled  bool   `json:"enabled"`
	Locked   bool   `json:"locked"`
	Warning  string `json:"warning"`
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
	RestartRequired bool     `json:"restartRequired"`
	Warnings        []string `json:"warnings,omitempty"`
}

type rawConfigRequest struct {
	YAML string `json:"yaml"`
}

type engineDefinition struct {
	Name     string
	Label    string
	Category string
	Locked   bool
	Warning  string
}

var managedEngines = []engineDefinition{
	{Name: "360search", Label: "360 搜索", Category: "general"},
	{Name: "360search videos", Label: "360 视频", Category: "videos"},
	{Name: "baidu", Label: "百度", Category: "general"},
	{Name: "baidu images", Label: "百度图片", Category: "images"},
	{Name: "baidu kaifa", Label: "百度开发者搜索", Category: "it"},
	{Name: "bilibili", Label: "哔哩哔哩", Category: "videos"},
	{Name: "bing", Label: "必应", Category: "general"},
	{Name: "bing images", Label: "必应图片", Category: "images"},
	{Name: "bing news", Label: "必应新闻", Category: "news"},
	{Name: "bing videos", Label: "必应视频", Category: "videos"},
	{Name: "chinaso news", Label: "中国搜索新闻", Category: "news", Locked: true, Warning: "该引擎的外部链接可能泄露用户搜索信息，存在隐私风险，因此已强制禁用。"},
	{Name: "iqiyi", Label: "爱奇艺", Category: "videos"},
	{Name: "quark", Label: "夸克", Category: "general"},
	{Name: "quark images", Label: "夸克图片", Category: "images"},
	{Name: "sogou", Label: "搜狗", Category: "general"},
	{Name: "sogou images", Label: "搜狗图片", Category: "images"},
	{Name: "sogou videos", Label: "搜狗视频", Category: "videos"},
	{Name: "sogou wechat", Label: "搜狗微信", Category: "social media"},
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
		Config:   configFromYAML(document),
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
	var input putConfigRequest
	if err := decodeJSON(request, &input, maxJSONBody); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateConfig(&input.Config); err != nil {
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
	applyConfig(document, input.Config)
	updated, err := yaml.Marshal(document)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": "encode settings.yml"})
		return
	}
	if err := atomicSaveWithBackup(handler.settingsPath, current, updated); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, saveResponse{
		Revision:        revision(updated),
		Saved:           true,
		RestartRequired: true,
		Warnings:        []string{"请在飞牛应用中心重启 SearXNG 使配置生效"},
	})
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
	writeJSON(response, http.StatusOK, saveResponse{Revision: revision(updated), Saved: true, RestartRequired: true})
}

func configFromYAML(document *yaml.Node) apiConfig {
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
	for _, definition := range managedEngines {
		disabled := engineDisabled(document, definition.Name)
		if definition.Locked {
			disabled = true
		}
		config.Engines = append(config.Engines, engineConfig{
			Name: definition.Name, Label: definition.Label, Category: definition.Category,
			Enabled: !disabled, Locked: definition.Locked, Warning: definition.Warning,
		})
	}
	return config
}

func validateConfig(config *apiConfig) error {
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
	knownEngines := make(map[string]engineDefinition, len(managedEngines))
	for _, definition := range managedEngines {
		knownEngines[definition.Name] = definition
	}
	if len(config.Engines) != len(managedEngines) {
		return errors.New("engines must contain the complete managed engine list")
	}
	seen := make(map[string]bool, len(config.Engines))
	for index := range config.Engines {
		engine := &config.Engines[index]
		definition, ok := knownEngines[engine.Name]
		if !ok || seen[engine.Name] {
			return fmt.Errorf("invalid or duplicate engine %q", engine.Name)
		}
		seen[engine.Name] = true
		if definition.Locked {
			engine.Enabled = false
		}
	}
	return nil
}

func applyConfig(document *yaml.Node, config apiConfig) {
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
	for _, engine := range config.Engines {
		setEngineDisabled(document, engine.Name, !engine.Enabled || engine.Name == "chinaso news")
	}
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

func engineDisabled(document *yaml.Node, name string) bool {
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		return true
	}
	for _, engine := range engines.Content {
		if scalarString(mappingValue(engine, "name"), "") == name {
			return scalarBool(mappingValue(engine, "disabled"), false)
		}
	}
	return true
}

func setEngineDisabled(document *yaml.Node, name string, disabled bool) {
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		engines = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		setPath(document, engines, "engines")
	}
	for _, engine := range engines.Content {
		if scalarString(mappingValue(engine, "name"), "") == name {
			setMappingValue(engine, "disabled", scalarNode(disabled))
			return
		}
	}
	engine := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingValue(engine, "name", scalarNode(name))
	setMappingValue(engine, "disabled", scalarNode(disabled))
	engines.Content = append(engines.Content, engine)
}
