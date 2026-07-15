package main

import "testing"

func TestParseOptionsConfigPaths(t *testing.T) {
	defaults, err := parseOptions(nil)
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}
	if defaults.settingsPath != "/config/settings.yml" {
		t.Fatalf("settings default = %q", defaults.settingsPath)
	}
	if defaults.brandingDir != "/config/branding" {
		t.Fatalf("branding default = %q", defaults.brandingDir)
	}

	custom, err := parseOptions([]string{"--settings", "/data/settings.yml", "--branding-dir", "/data/branding"})
	if err != nil {
		t.Fatalf("parse custom paths: %v", err)
	}
	if custom.settingsPath != "/data/settings.yml" || custom.brandingDir != "/data/branding" {
		t.Fatalf("unexpected custom paths: %+v", custom)
	}
}
