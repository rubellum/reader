package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rubellum/reader/internal/server"
	"github.com/rubellum/reader/internal/worktree"
)

// stringSlice は複数回指定可能なフラグ用の型
type stringSlice []string

func (s *stringSlice) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringSlice) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type cliReadRoot struct {
	Path     string
	SortDesc bool
	FlagName string
}

type cliReadRoots []cliReadRoot

func (r *cliReadRoots) String() string {
	parts := make([]string, 0, len(*r))
	for _, root := range *r {
		parts = append(parts, root.Path)
	}
	return strings.Join(parts, ", ")
}

func (r *cliReadRoots) add(value string, sortDesc bool, flagName string) error {
	*r = append(*r, cliReadRoot{Path: value, SortDesc: sortDesc, FlagName: flagName})
	return nil
}

type readRootFlag struct {
	roots    *cliReadRoots
	sortDesc bool
	flagName string
}

func (f readRootFlag) String() string {
	if f.roots == nil {
		return ""
	}
	return f.roots.String()
}

func (f readRootFlag) Set(value string) error {
	return f.roots.add(value, f.sortDesc, f.flagName)
}

type countFlag int

func (c *countFlag) String() string {
	return strconv.Itoa(int(*c))
}

func (c *countFlag) Set(value string) error {
	if value == "" {
		*c++
		return nil
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*c = countFlag(count)
	return nil
}

func (c *countFlag) IsBoolFlag() bool {
	return true
}

const (
	defaultPort       = 3333
	defaultHost       = "127.0.0.1"
	defaultConfigFile = "config.json"
	defaultArchiveDir = "archive"
)

// デフォルトパターン（include/exclude 共に未指定時に適用）
var (
	defaultIncludes = []string{
		"*.md",
		"*.txt",
		"*.html",
		"*.htm",
	}
	defaultExcludes = []string{
		"node_modules/*",
		"vendor/*",
		".git/*",
		"__pycache__/*",
		".venv/*",
		"venv/*",
		"dist/*",
		"build/*",
	}
)

func main() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	// フラグ定義
	host := flag.String("host", defaultHost, "バインドアドレス")
	port := flag.Int("port", defaultPort, "ポート番号 (1-65535)")
	var includes stringSlice
	flag.Var(&includes, "include", "包含パターン（複数指定可）")
	var excludes stringSlice
	flag.Var(&excludes, "exclude", "除外パターン（複数指定可）")
	var readRoots cliReadRoots
	flag.Var(readRootFlag{roots: &readRoots, flagName: "-read"}, "read", "閲覧フォルダ（複数指定可。指定時は起動ディレクトリを表示しない）")
	flag.Var(readRootFlag{roots: &readRoots, sortDesc: true, flagName: "-read-r"}, "read-r", "閲覧フォルダ（複数指定可。サイドバーの並び順を降順にする）")
	var writeRoots cliReadRoots
	flag.Var(readRootFlag{roots: &writeRoots, flagName: "-write"}, "write", "書き込みフォルダ（複数指定可。編集 UI を有効化）")
	flag.Var(readRootFlag{roots: &writeRoots, sortDesc: true, flagName: "-write-r"}, "write-r", "書き込みフォルダ（複数指定可。サイドバーの並び順を降順にする）")
	var verbosity countFlag
	flag.Var(&verbosity, "v", "詳細ログ（-v, -vv, -vvv）")
	configPath := flag.String("config", "", "設定ファイル（JSON）。未指定時は ./config.json を自動検出")
	archiveDir := flag.String("archive", defaultArchiveDir, "アーカイブフォルダ")
	noOpen := flag.Bool("no-open", false, "起動時にブラウザを自動で開かない")

	// カスタムUsage
	flag.Usage = func() {
		fmt.Println("reader - Git リポジトリ用ドキュメントビューア")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Println("  reader [options] [directory]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  -host string")
		fmt.Printf("        バインドアドレス (default %s)\n", defaultHost)
		fmt.Println("  -port int")
		fmt.Printf("        ポート番号 (default %d)\n", defaultPort)
		fmt.Println("  -include string")
		fmt.Println("        包含パターン（複数指定可、ルート相対）")
		fmt.Println("  -exclude string")
		fmt.Println("        除外パターン（複数指定可、ルート相対）")
		fmt.Println("  -read string")
		fmt.Println("        閲覧フォルダ（複数指定可。指定時は起動ディレクトリを表示しない）")
		fmt.Println("  -read-r string")
		fmt.Println("        閲覧フォルダ（複数指定可。サイドバーの並び順を降順にする）")
		fmt.Println("  -write string")
		fmt.Println("        書き込みフォルダ（複数指定可。編集 UI を有効化）")
		fmt.Println("  -write-r string")
		fmt.Println("        書き込みフォルダ（複数指定可。サイドバーの並び順を降順にする）")
		fmt.Println("  -v")
		fmt.Println("        詳細ログ（-v, -vv, -vvv）")
		fmt.Println("  -config string")
		fmt.Printf("        設定ファイル（JSON）。未指定時は ./%s を自動検出\n", defaultConfigFile)
		fmt.Println("  -archive string")
		fmt.Printf("        アーカイブフォルダ (default %s)\n", defaultArchiveDir)
		fmt.Println("  -no-open")
		fmt.Println("        起動時にブラウザを自動で開かない")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  reader                                       # カレントディレクトリを表示")
		fmt.Println("  reader /path/to/repo                         # 指定ディレクトリを表示")
		fmt.Println("  reader docs                                  # サブディレクトリをルートに")
		fmt.Println("  reader -port 8080                            # ポート8080で起動")
		fmt.Println("  reader -include \"*.md\"                       # Markdownのみ表示")
		fmt.Println("  reader -include \"*.md\" -include \"*.txt\"      # 複数の include")
		fmt.Println("  reader -exclude \"vendor/*\"                   # vendorを除外")
		fmt.Println("  reader -include \"docs/*.md\" -exclude \"docs/draft/*\"  # 組み合わせ")
		fmt.Println("  reader -read /tmp/reference /path/to/repo    # /tmp/reference のみを閲覧ツリーに表示")
		fmt.Println("  reader -read /tmp/a -read-r /tmp/b .         # 指定順で閲覧ツリーを表示（/tmp/b は降順）")
		fmt.Println("  reader -write /tmp/notes /path/to/repo       # 編集 UI 付きで起動")
		fmt.Println("  reader -write /tmp/a -write-r /tmp/b .       # 指定順で編集ツリーを表示（/tmp/b は降順）")
		fmt.Println("  reader -archive archived .                   # archived/ 配下にファイルをアーカイブ")
		fmt.Println("  reader -vvv                                  # 詳細ログを有効化")
		fmt.Println()
		fmt.Println("Note:")
		fmt.Println("  Git リポジトリ必須。git ls-files で管理されたファイルのみ表示します。")
		fmt.Println("  -include / -exclude のいずれも未指定時はデフォルト (*.md, *.txt, *.html, *.htm と")
		fmt.Println("  node_modules/vendor などの定番除外) が適用されます。")
	}

	flag.CommandLine.Parse(expandVerbosityArgs(os.Args[1:]))

	// 設定ファイルの読み込み
	// -config 明示指定時は失敗で終了、未指定時は ./config.json を自動検出（無ければ無視）
	var cfg *Config
	cfgFilePath := *configPath
	cfgExplicit := cfgFilePath != ""
	if !cfgExplicit {
		cfgFilePath = defaultConfigFile
	}
	loaded, err := loadConfig(cfgFilePath)
	if err != nil {
		if cfgExplicit || !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
			os.Exit(1)
		}
	} else {
		cfg = loaded
	}

	// CLI で明示指定されたフラグを記録（config からの上書き対象を除外するため）
	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	// CLI 未指定の値に config の値を適用
	if cfg != nil {
		if !setFlags["host"] && cfg.Host != nil {
			*host = *cfg.Host
		}
		if !setFlags["port"] && cfg.Port != nil {
			*port = *cfg.Port
		}
		if len(includes) == 0 && len(cfg.Include) > 0 {
			includes = cfg.Include
		}
		if len(excludes) == 0 && len(cfg.Exclude) > 0 {
			excludes = cfg.Exclude
		}
		if !setFlags["read"] && !setFlags["read-r"] {
			if cfg.Read != nil {
				_ = readRoots.add(*cfg.Read, false, "-read")
			}
			if cfg.ReadR != nil {
				_ = readRoots.add(*cfg.ReadR, true, "-read-r")
			}
		}
		if !setFlags["write"] && !setFlags["write-r"] {
			if cfg.Write != nil {
				_ = writeRoots.add(*cfg.Write, false, "-write")
			}
			if cfg.WriteR != nil {
				_ = writeRoots.add(*cfg.WriteR, true, "-write-r")
			}
		}
		if !setFlags["archive"] && cfg.Archive != nil {
			*archiveDir = *cfg.Archive
		}
		if !setFlags["v"] && cfg.Verbosity != nil {
			verbosity = countFlag(*cfg.Verbosity)
		}
	}

	// ターゲットディレクトリの決定（位置引数 > config.dir > "."）
	var targetDir string

	if flag.NArg() >= 1 {
		targetDir = flag.Arg(0)
	} else if cfg != nil && cfg.Dir != nil {
		targetDir = *cfg.Dir
	} else {
		targetDir = "."
	}

	// include/exclude が両方未指定ならデフォルトパターンを適用
	useDefaultPatterns := len(includes) == 0 && len(excludes) == 0

	// ポート番号バリデーション
	if *port < 1 || *port > 65535 {
		fmt.Fprintf(os.Stderr, "エラー: ポート番号は1-65535の範囲で指定してください: %d\n", *port)
		os.Exit(1)
	}
	if strings.TrimSpace(*archiveDir) == "" {
		fmt.Fprintln(os.Stderr, "エラー: アーカイブフォルダを空にはできません")
		os.Exit(1)
	}
	// パターンバリデーション
	for _, p := range append(append([]string{}, includes...), excludes...) {
		if _, err := filepath.Match(p, "test"); err != nil {
			fmt.Fprintf(os.Stderr, "エラー: 無効なパターンです: %s\n", p)
			os.Exit(1)
		}
	}

	// ディレクトリの存在確認
	info, err := os.Stat(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "エラー: ディレクトリが存在しません: %s\n", targetDir)
		} else {
			fmt.Fprintf(os.Stderr, "エラー: %v\n", err)
		}
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "エラー: ディレクトリではありません: %s\n", targetDir)
		os.Exit(1)
	}

	// Git リポジトリの検出（diff 機能の前提）
	gitRoot, err := worktree.GitRoot(targetDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: Git リポジトリではありません: %s\n", targetDir)
		os.Exit(1)
	}

	// targetDir をルートディレクトリとして開く
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: パスの解決に失敗しました: %v\n", err)
		os.Exit(1)
	}

	// -read の検証と絶対パス化。指定時は targetDir ではなく -read のみを閲覧ルートにする。
	readRootOptions := []server.RootOption{{BasePath: absTargetDir}}
	if len(readRoots) > 0 {
		readRootOptions = make([]server.RootOption, 0, len(readRoots))
		for _, root := range readRoots {
			absReadDir, err := validateOptionDir(root.FlagName, root.Path, false)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			readRootOptions = append(readRootOptions, server.RootOption{
				BasePath: absReadDir,
				SortDesc: root.SortDesc,
			})
		}
	}

	// -write の検証と絶対パス化
	writeRootOptions := make([]server.RootOption, 0, len(writeRoots))
	for _, root := range writeRoots {
		absWriteDir, err := validateOptionDir(root.FlagName, root.Path, true)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		writeRootOptions = append(writeRootOptions, server.RootOption{
			BasePath: absWriteDir,
			SortDesc: root.SortDesc,
		})
	}

	// ポート確保（フォールバック付き）
	listener, fallback, err := listenWithFallback(*host, *port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: ポートを確保できません: %v\n", err)
		os.Exit(1)
	}
	if fallback {
		fmt.Fprintf(os.Stderr, "ポート %d は使用中です。空きポートにフォールバックします。\n", *port)
	}

	actualAddr := listener.Addr().String()
	url := fmt.Sprintf("http://%s", actualAddr)

	// 使用するパターンを決定
	effectiveInclude := includes
	effectiveExclude := excludes
	if useDefaultPatterns {
		effectiveInclude = defaultIncludes
		effectiveExclude = defaultExcludes
	}

	// サーバー起動（指定ディレクトリをルートとして使用、-read/-write 指定時は追加ルートも追加）
	srv := server.NewWithOptions(server.Options{
		ReadRoots:  readRootOptions,
		WriteRoots: writeRootOptions,
		Include:    effectiveInclude,
		Exclude:    effectiveExclude,
		Verbose:    verbosity > 0,
		ArchiveDir: *archiveDir,
	})

	if !*noOpen {
		// 少し待ってからブラウザを開く
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(url)
		}()
	}

	fmt.Printf("reader サーバーを起動しました: %s\n", url)
	fmt.Printf("ルートディレクトリ: %s\n", absTargetDir)
	if absTargetDir != gitRoot {
		fmt.Printf("Git root: %s\n", gitRoot)
	}
	if len(readRoots) > 0 {
		for i, root := range readRootOptions {
			order := "昇順"
			if root.SortDesc {
				order = "降順"
			}
			fmt.Printf("閲覧フォルダ%d: %s (%s)\n", i+1, root.BasePath, order)
		}
	}
	for i, root := range writeRootOptions {
		order := "昇順"
		if root.SortDesc {
			order = "降順"
		}
		fmt.Printf("書き込みフォルダ%d: %s (%s)\n", i+1, root.BasePath, order)
	}
	if useDefaultPatterns {
		fmt.Println("デフォルトパターン適用中 (include: *.md, *.txt, *.html, *.htm / exclude: node_modules, vendor 等)")
	}
	fmt.Printf("アーカイブフォルダ: %s\n", *archiveDir)
	fmt.Println("終了するには Ctrl+C を押してください")

	if err := srv.StartWithListener(listener); err != nil {
		fmt.Fprintf(os.Stderr, "サーバーエラー: %v\n", err)
		os.Exit(1)
	}
}

// openBrowser はデフォルトブラウザでURLを開く（macOS用）
func openBrowser(url string) {
	exec.Command("open", url).Start()
}

func validateOptionDir(flagName, dir string, createMissing bool) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if createMissing && os.IsNotExist(err) {
			if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
				return "", fmt.Errorf("エラー: %s のディレクトリを作成できません: %v", flagName, mkErr)
			}
			info, err = os.Stat(dir)
		}
		if err != nil {
			return "", fmt.Errorf("エラー: %s のディレクトリにアクセスできません: %v", flagName, err)
		}
	}
	if !info.IsDir() {
		return "", fmt.Errorf("エラー: %s はディレクトリを指定してください: %s", flagName, dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("エラー: %s のパス解決に失敗しました: %v", flagName, err)
	}
	return abs, nil
}

// listenWithFallback は指定ポートでリッスンを試み、失敗時は連番でポートをインクリメントして再試行する
func listenWithFallback(host string, port int) (net.Listener, bool, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		return listener, false, nil
	}

	// フォールバック: ポートをインクリメントしながら試行
	for p := port + 1; p <= port+100 && p <= 65535; p++ {
		addr = fmt.Sprintf("%s:%d", host, p)
		listener, err = net.Listen("tcp", addr)
		if err == nil {
			return listener, true, nil
		}
	}
	return nil, false, fmt.Errorf("ポート %d-%d はすべて使用中です", port, port+100)
}

func expandVerbosityArgs(args []string) []string {
	expanded := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.HasPrefix(arg, "-vv") && !strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") {
			count := 0
			for i := 1; i < len(arg); i++ {
				if arg[i] != 'v' {
					count = 0
					break
				}
				count++
			}
			if count > 0 {
				for i := 0; i < count; i++ {
					expanded = append(expanded, "-v")
				}
				continue
			}
		}
		expanded = append(expanded, arg)
	}
	return expanded
}
