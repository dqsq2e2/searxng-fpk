package server

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type engineDefinition struct {
	Name            string
	Label           string
	Shortcut        string
	Category        string
	DefaultDisabled bool
	DefaultInactive bool
}

var localizedEngineLabels = map[string]string{
	"360search": "360 搜索", "360search videos": "360 视频", "baidu": "百度", "baidu images": "百度图片",
	"baidu kaifa": "百度开发者搜索", "bilibili": "哔哩哔哩", "bing": "必应", "bing images": "必应图片",
	"bing news": "必应新闻", "bing videos": "必应视频", "chinaso news": "中国搜索新闻", "iqiyi": "爱奇艺",
	"quark": "夸克", "quark images": "夸克图片", "sogou": "搜狗", "sogou images": "搜狗图片",
	"sogou videos": "搜狗视频", "sogou wechat": "搜狗微信",
}

func loadEngineCatalog(filename string) ([]engineDefinition, error) {
	_, document, err := readYAMLDocument(filename)
	if err != nil {
		return nil, fmt.Errorf("read default settings: %w", err)
	}
	engines := lookupPath(document, "engines")
	if engines == nil || engines.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("default settings %s has no engines sequence", filename)
	}
	catalog := make([]engineDefinition, 0, len(engines.Content))
	seen := make(map[string]struct{}, len(engines.Content))
	for _, engine := range engines.Content {
		name := scalarString(mappingValue(engine, "name"), "")
		if name == "" {
			return nil, fmt.Errorf("default engine without name")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate default engine %q", name)
		}
		seen[name] = struct{}{}
		catalog = append(catalog, engineDefinition{
			Name:            name,
			Label:           localizedEngineLabel(name),
			Shortcut:        scalarString(mappingValue(engine, "shortcut"), ""),
			Category:        engineCategory(engine),
			DefaultDisabled: scalarBool(mappingValue(engine, "disabled"), false),
			DefaultInactive: scalarBool(mappingValue(engine, "inactive"), false),
		})
	}
	if len(catalog) == 0 {
		return nil, fmt.Errorf("default settings %s has an empty engine catalog", filename)
	}
	return catalog, nil
}

func localizedEngineLabel(name string) string {
	if label := localizedEngineLabels[name]; label != "" {
		return label
	}
	return name
}

func engineCategory(engine *yaml.Node) string {
	categories := mappingValue(engine, "categories")
	if categories == nil {
		return "other"
	}
	if categories.Kind == yaml.SequenceNode {
		values := make([]string, 0, len(categories.Content))
		for _, category := range categories.Content {
			if value := scalarString(category, ""); value != "" {
				values = append(values, value)
			}
		}
		if len(values) > 0 {
			return strings.Join(values, ", ")
		}
	}
	if value := scalarString(categories, ""); value != "" {
		return value
	}
	return "other"
}
