package main

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultPatternsApplied(t *testing.T) {
	if !reflect.DeepEqual(defaultIncludes, []string{"*.md", "*.txt", "*.html", "*.htm"}) {
		t.Fatalf("unexpected default includes: %v", defaultIncludes)
	}

	// 両方未指定ならデフォルト適用
	var includes, excludes stringSlice
	if !(len(includes) == 0 && len(excludes) == 0) {
		t.Fatalf("expected both empty initially")
	}

	effectiveInclude := []string(includes)
	effectiveExclude := []string(excludes)
	if len(effectiveInclude) == 0 && len(effectiveExclude) == 0 {
		effectiveInclude = defaultIncludes
		effectiveExclude = defaultExcludes
	}
	if !reflect.DeepEqual(effectiveInclude, defaultIncludes) {
		t.Fatalf("expected include default %v, got %v", defaultIncludes, effectiveInclude)
	}
	if !reflect.DeepEqual(effectiveExclude, defaultExcludes) {
		t.Fatalf("expected exclude default %v, got %v", defaultExcludes, effectiveExclude)
	}

	// include だけ指定すればデフォルトは適用されない
	includes = stringSlice{"*.go"}
	if len(includes) == 0 && len(excludes) == 0 {
		t.Fatalf("expected default NOT to apply when include is set")
	}

	// exclude だけ指定すればデフォルトは適用されない
	includes = nil
	excludes = stringSlice{"build/*"}
	if len(includes) == 0 && len(excludes) == 0 {
		t.Fatalf("expected default NOT to apply when exclude is set")
	}
}

func TestListenWithFallback_AvailablePort(t *testing.T) {
	// 空きポートを見つけてテスト
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := tmp.Addr().(*net.TCPAddr).Port
	tmp.Close()

	listener, fallback, err := listenWithFallback("127.0.0.1", port)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer listener.Close()

	if fallback {
		t.Fatalf("expected no fallback for available port")
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	if actualPort != port {
		t.Fatalf("expected port %d, got %d", port, actualPort)
	}
}

func TestListenWithFallback_OccupiedPort(t *testing.T) {
	// ポートを先に占有する
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	defer occupied.Close()
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port

	listener, fallback, err := listenWithFallback("127.0.0.1", occupiedPort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer listener.Close()

	if !fallback {
		t.Fatalf("expected fallback for occupied port")
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	if actualPort != occupiedPort+1 {
		t.Fatalf("expected port %d, got %d", occupiedPort+1, actualPort)
	}
}

func TestExpandVerbosityArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "empty", in: nil, want: []string{}},
		{name: "single -v unchanged", in: []string{"-v"}, want: []string{"-v"}},
		{name: "-vv expands to two -v", in: []string{"-vv"}, want: []string{"-v", "-v"}},
		{name: "-vvv expands to three -v", in: []string{"-vvv"}, want: []string{"-v", "-v", "-v"}},
		{name: "preserves other args", in: []string{"-port", "8080", "-vv", "."}, want: []string{"-port", "8080", "-v", "-v", "."}},
		{name: "ignores --vv (long flag form)", in: []string{"--vv"}, want: []string{"--vv"}},
		{name: "ignores -v=2 (value form)", in: []string{"-v=2"}, want: []string{"-v=2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandVerbosityArgs(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expandVerbosityArgs(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateOptionDirCreatesMissingWriteDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "notes")

	got, err := validateOptionDir("-write", dir, true)
	if err != nil {
		t.Fatalf("validateOptionDir returned error: %v", err)
	}
	if got != dir {
		t.Fatalf("expected %s, got %s", dir, got)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("expected directory to be created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected created path to be a directory")
	}
}

func TestValidateOptionDirReadDirStillRequiresExistingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	if _, err := validateOptionDir("-read", dir, false); err == nil {
		t.Fatalf("expected missing read directory to fail")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected read directory not to be created, got err=%v", err)
	}
}

func TestValidateOptionDirRejectsFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	if _, err := validateOptionDir("-write", file, true); err == nil {
		t.Fatalf("expected file path to be rejected")
	}
}

func TestListenWithFallback_FallbackListenerIsUsable(t *testing.T) {
	// ポートを先に占有する
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to occupy port: %v", err)
	}
	defer occupied.Close()
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port

	listener, _, err := listenWithFallback("127.0.0.1", occupiedPort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer listener.Close()

	// フォールバックで取得したリスナーに接続できることを確認
	addr := listener.Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect to fallback listener: %v", err)
	}
	conn.Close()
}
