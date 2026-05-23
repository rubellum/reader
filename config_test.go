package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadConfig_AllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{
		"host": "0.0.0.0",
		"port": 8080,
		"include": ["*.md", "*.txt"],
		"exclude": ["build/*"],
		"read": "/tmp/reference",
		"read-r": "/tmp/reference-desc",
		"write": "/tmp/notes",
		"write-r": "/tmp/notes-desc",
		"archive": "archived",
		"verbosity": 2,
		"dir": "/tmp/repo"
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Host == nil || *cfg.Host != "0.0.0.0" {
		t.Errorf("host = %v, want 0.0.0.0", cfg.Host)
	}
	if cfg.Port == nil || *cfg.Port != 8080 {
		t.Errorf("port = %v, want 8080", cfg.Port)
	}
	if !reflect.DeepEqual(cfg.Include, []string{"*.md", "*.txt"}) {
		t.Errorf("include = %v", cfg.Include)
	}
	if !reflect.DeepEqual(cfg.Exclude, []string{"build/*"}) {
		t.Errorf("exclude = %v", cfg.Exclude)
	}
	if cfg.Read == nil || *cfg.Read != "/tmp/reference" {
		t.Errorf("read = %v, want /tmp/reference", cfg.Read)
	}
	if cfg.ReadR == nil || *cfg.ReadR != "/tmp/reference-desc" {
		t.Errorf("read-r = %v, want /tmp/reference-desc", cfg.ReadR)
	}
	if cfg.Write == nil || *cfg.Write != "/tmp/notes" {
		t.Errorf("write = %v, want /tmp/notes", cfg.Write)
	}
	if cfg.WriteR == nil || *cfg.WriteR != "/tmp/notes-desc" {
		t.Errorf("write-r = %v, want /tmp/notes-desc", cfg.WriteR)
	}
	if cfg.Archive == nil || *cfg.Archive != "archived" {
		t.Errorf("archive = %v, want archived", cfg.Archive)
	}
	if cfg.Verbosity == nil || *cfg.Verbosity != 2 {
		t.Errorf("verbosity = %v, want 2", cfg.Verbosity)
	}
	if cfg.Dir == nil || *cfg.Dir != "/tmp/repo" {
		t.Errorf("dir = %v, want /tmp/repo", cfg.Dir)
	}
}

func TestLoadConfig_PartialFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"port": 9999}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Port == nil || *cfg.Port != 9999 {
		t.Errorf("port = %v, want 9999", cfg.Port)
	}
	if cfg.Host != nil {
		t.Errorf("host should be nil, got %v", *cfg.Host)
	}
	if cfg.Include != nil {
		t.Errorf("include should be nil, got %v", cfg.Include)
	}
}

func TestLoadConfig_NotFound(t *testing.T) {
	_, err := loadConfig(filepath.Join(t.TempDir(), "missing.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got %v", err)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
}
