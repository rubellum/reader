package server

import (
	"embed"
	"html/template"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rubellum/reader/internal/diff"
	"github.com/rubellum/reader/internal/document"
	"github.com/rubellum/reader/internal/tree"
	"github.com/rubellum/reader/internal/worktree"
)

//go:embed templates/*
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// rootCtx はサーバーが扱うルートディレクトリの解決情報を保持する。
// basePath はユーザーが指定したルートディレクトリ（API のパスはこの相対）。
// gitRoot は git リポジトリのルート（worktree 操作・diff 用、git管理外なら basePath と同じ）。
// relSubdir は basePath が gitRoot からどれだけ深い位置にあるか（"." なら同一）。
type rootCtx struct {
	basePath  string
	gitRoot   string
	relSubdir string
	renderer  *document.Renderer
}

type readRoot struct {
	id       string
	ctx      rootCtx
	sortDesc bool
}

func newRootCtx(basePath string) rootCtx {
	absBase := resolveAbs(basePath)
	gitRoot := absBase
	if gr, err := worktree.GitRoot(absBase); err == nil {
		gitRoot = resolveAbs(gr)
	}
	relSubdir := "."
	if rel, err := filepath.Rel(gitRoot, absBase); err == nil && rel != "" {
		relSubdir = rel
	}
	return rootCtx{
		basePath:  absBase,
		gitRoot:   gitRoot,
		relSubdir: relSubdir,
		renderer:  document.NewRenderer(absBase),
	}
}

// worktreeBaseDir は指定 worktree における basePath 相当のディレクトリを返す。
func (c *rootCtx) worktreeBaseDir(wt *worktree.Worktree) string {
	if c.relSubdir == "." || c.relSubdir == "" {
		return wt.Path
	}
	return filepath.Join(wt.Path, c.relSubdir)
}

// Server はHTTPサーバーを表す。
// readRoots は閲覧用ルート、write はオプショナルな編集用ルート。
type Server struct {
	echo       *echo.Echo
	readRoots  []readRoot
	writeRoots []readRoot
	include    []string
	exclude    []string
}

// RootOption は1つの閲覧ルートの設定を表す。
type RootOption struct {
	BasePath string
	SortDesc bool
}

// Options はサーバーが扱う各ルートと表示順の設定を表す。
type Options struct {
	ReadBasePath      string
	ReadRoots         []RootOption
	ExtraReadBasePath string
	WriteBasePath     string
	WriteRoots        []RootOption
	Include           []string
	Exclude           []string
	Verbose           bool
	ReadSortDesc      bool
	ExtraReadSortDesc bool
	WriteSortDesc     bool
}

// NewWithOptions は閲覧用ルート、追加閲覧ルート、書き込みルートを持つServerを作成する。
func NewWithOptions(opts Options) *Server {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.Recover())
	if opts.Verbose {
		e.Use(middleware.Logger())
	}

	s := &Server{
		echo:    e,
		include: opts.Include,
		exclude: opts.Exclude,
	}
	readOpts := opts.ReadRoots
	if len(readOpts) == 0 {
		readOpts = []RootOption{{BasePath: opts.ReadBasePath, SortDesc: opts.ReadSortDesc}}
		if opts.ExtraReadBasePath != "" {
			readOpts = append(readOpts, RootOption{BasePath: opts.ExtraReadBasePath, SortDesc: opts.ExtraReadSortDesc})
		}
	}
	for i, ro := range readOpts {
		id := "read"
		if i > 0 {
			id = "read-" + strconv.Itoa(i+1)
		}
		if len(opts.ReadRoots) == 0 && i == 1 {
			id = "extra-read"
		}
		s.readRoots = append(s.readRoots, readRoot{
			id:       id,
			ctx:      newRootCtx(ro.BasePath),
			sortDesc: ro.SortDesc,
		})
	}
	writeOpts := opts.WriteRoots
	if len(writeOpts) == 0 && opts.WriteBasePath != "" {
		writeOpts = []RootOption{{BasePath: opts.WriteBasePath, SortDesc: opts.WriteSortDesc}}
	}
	for i, ro := range writeOpts {
		id := "write"
		if i > 0 {
			id = "write-" + strconv.Itoa(i+1)
		}
		s.writeRoots = append(s.writeRoots, readRoot{
			id:       id,
			ctx:      newRootCtx(ro.BasePath),
			sortDesc: ro.SortDesc,
		})
	}

	s.setupRoutes()
	return s
}

// rootByName は API クエリパラメータ root の値から rootCtx を返す。
// "write" で write が設定されていなければエラー。空文字は先頭の閲覧ルートを返す。
func (s *Server) rootByName(name string) (*rootCtx, error) {
	if name == "write" {
		if len(s.writeRoots) == 0 {
			return nil, echo.NewHTTPError(http.StatusBadRequest, "書き込みルートが設定されていません")
		}
		return &s.writeRoots[0].ctx, nil
	}
	for i := range s.writeRoots {
		if s.writeRoots[i].id == name {
			return &s.writeRoots[i].ctx, nil
		}
	}
	if name == "" {
		if len(s.readRoots) == 0 {
			return nil, echo.NewHTTPError(http.StatusInternalServerError, "閲覧ルートが設定されていません")
		}
		return &s.readRoots[0].ctx, nil
	}
	for i := range s.readRoots {
		if s.readRoots[i].id == name {
			return &s.readRoots[i].ctx, nil
		}
	}
	return nil, echo.NewHTTPError(http.StatusBadRequest, "閲覧ルートが設定されていません")
}

func (s *Server) sortDescByRootName(name string) bool {
	if name == "write" {
		if len(s.writeRoots) > 0 {
			return s.writeRoots[0].sortDesc
		}
		return false
	}
	for _, wr := range s.writeRoots {
		if wr.id == name {
			return wr.sortDesc
		}
	}
	if name == "" && len(s.readRoots) > 0 {
		return s.readRoots[0].sortDesc
	}
	for _, rr := range s.readRoots {
		if rr.id == name {
			return rr.sortDesc
		}
	}
	return false
}

// setupRoutes はルートを設定する
func (s *Server) setupRoutes() {
	// メインページ
	s.echo.GET("/", s.handleIndex)

	// API
	s.echo.GET("/api/config", s.handleConfig)
	s.echo.GET("/api/tree", s.handleTree)
	s.echo.GET("/api/file-meta", s.handleFileMeta)
	s.echo.GET("/api/file", s.handleFile)
	s.echo.PUT("/api/file", s.handleFileWrite)
	s.echo.GET("/api/worktrees", s.handleWorktrees)
	s.echo.GET("/api/diff", s.handleDiff)
	s.echo.GET("/api/raw", s.handleRaw)

	// 静的ファイル
	staticSubFS, _ := fs.Sub(staticFS, "static")
	s.echo.StaticFS("/static", staticSubFS)
}

// handleConfig はクライアントが UI を初期化するための設定を返す。
func (s *Server) handleConfig(c echo.Context) error {
	readRoots := make([]map[string]interface{}, 0, len(s.readRoots))
	for _, rr := range s.readRoots {
		readRoots = append(readRoots, map[string]interface{}{
			"id":       rr.id,
			"name":     filepath.Base(rr.ctx.basePath),
			"sortDesc": rr.sortDesc,
		})
	}
	writeRoots := make([]map[string]interface{}, 0, len(s.writeRoots))
	for _, wr := range s.writeRoots {
		writeRoots = append(writeRoots, map[string]interface{}{
			"id":       wr.id,
			"name":     filepath.Base(wr.ctx.basePath),
			"sortDesc": wr.sortDesc,
		})
	}
	cfg := map[string]interface{}{
		"writeEnabled":      len(s.writeRoots) > 0,
		"extraReadEnabled":  len(s.readRoots) > 1,
		"readRoots":         readRoots,
		"writeRoots":        writeRoots,
		"readRootName":      "",
		"readRootSortDesc":  false,
		"writeRootSortDesc": false,
		"extraReadSortDesc": false,
	}
	if len(s.readRoots) > 0 {
		cfg["readRootName"] = filepath.Base(s.readRoots[0].ctx.basePath)
		cfg["readRootSortDesc"] = s.readRoots[0].sortDesc
	}
	if len(s.readRoots) > 1 {
		cfg["extraReadRootName"] = filepath.Base(s.readRoots[1].ctx.basePath)
		cfg["extraReadSortDesc"] = s.readRoots[1].sortDesc
	}
	if len(s.writeRoots) > 0 {
		cfg["writeRootName"] = filepath.Base(s.writeRoots[0].ctx.basePath)
		cfg["writeRootSortDesc"] = s.writeRoots[0].sortDesc
	}
	return c.JSON(http.StatusOK, cfg)
}

// handleIndex はメインページを返す
func (s *Server) handleIndex(c echo.Context) error {
	tmplContent, err := templatesFS.ReadFile("templates/index.html")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "テンプレートの読み込みに失敗しました")
	}

	tmpl, err := template.New("index").Parse(string(tmplContent))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "テンプレートのパースに失敗しました")
	}

	c.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	return tmpl.Execute(c.Response(), nil)
}

// handleTree はツリー構造を返す（全 worktree の統合ツリー）。
// クエリ `root=write` で書き込み用ルートのツリーを返す。
func (s *Server) handleTree(c echo.Context) error {
	rootNameParam := c.QueryParam("root")
	ctx, err := s.rootByName(rootNameParam)
	if err != nil {
		return err
	}
	opts := tree.BuildOptions{
		Include:  s.include,
		Exclude:  s.exclude,
		SortDesc: s.sortDescByRootName(rootNameParam),
	}

	// worktree 一覧取得（git root から取得することで basePath がサブディレクトリでも全 worktree を見られる）
	worktrees, err := worktree.List(ctx.gitRoot)
	if err != nil || len(worktrees) == 0 {
		// worktree が取得できない場合はカレントの git ls-files を使用
		files, ferr := buildFileListFallback(ctx.basePath)
		if ferr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "ファイル一覧の取得に失敗しました")
		}
		rootName := filepath.Base(ctx.basePath)
		treeData := tree.BuildFromGitFiles(rootName, files, opts)
		annotateTreeFileMeta(treeData, ctx.basePath)
		return c.JSON(http.StatusOK, treeData)
	}

	rootName := filepath.Base(ctx.basePath)
	var merged *tree.TreeItem

	for _, wt := range worktrees {
		wtBase := ctx.worktreeBaseDir(&wt)
		if _, statErr := os.Stat(wtBase); statErr != nil {
			// このworktreeにはbasePath相当のディレクトリが無いのでスキップ
			continue
		}
		files, err := tree.GitFiles(wtBase)
		if err != nil {
			continue
		}
		wtTree := tree.BuildFromGitFiles(rootName, files, opts)

		if merged == nil {
			setWorktreesAll(wtTree, wt.Name)
			merged = wtTree
		} else {
			merged = tree.MergeTreeItemsWithOptions(merged, wtTree, wt.Name, opts)
		}
	}

	if merged == nil {
		merged = &tree.TreeItem{
			Name:  rootName,
			Path:  "",
			IsDir: true,
		}
	}

	metaBaseDir, err := s.resolveWorktreeBaseForCtx(ctx, c.QueryParam("metaWorktree"))
	if err == nil {
		annotateTreeFileMeta(merged, metaBaseDir)
	}

	return c.JSON(http.StatusOK, merged)
}

// buildFileListFallback は git 管理外のディレクトリで全ファイルを再帰的に列挙する。
// 隠しディレクトリ・ファイル（.git など）は除外する。
func buildFileListFallback(basePath string) ([]string, error) {
	var files []string
	err := filepath.Walk(basePath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // permission errors etc は無視して継続
		}
		name := info.Name()
		if info.IsDir() {
			if p != basePath && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, relErr := filepath.Rel(basePath, p)
		if relErr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// setWorktreesAll はツリー内の全ファイルに worktree 名を設定する
func setWorktreesAll(item *tree.TreeItem, name string) {
	if !item.IsDir {
		item.Worktrees = []string{name}
	}
	for _, child := range item.Children {
		setWorktreesAll(child, name)
	}
}

func annotateTreeFileMeta(item *tree.TreeItem, baseDir string) {
	if item == nil {
		return
	}
	if !item.IsDir {
		info, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(item.Path)))
		if err != nil || info.IsDir() {
			item.ModifiedAtMs = 0
			item.Size = 0
			return
		}
		item.ModifiedAtMs = info.ModTime().UnixMilli()
		item.Size = info.Size()
		return
	}
	for _, child := range item.Children {
		annotateTreeFileMeta(child, baseDir)
	}
}

type fileMeta struct {
	Path         string `json:"path"`
	ModifiedAtMs int64  `json:"modifiedAtMs"`
	Size         int64  `json:"size"`
}

// handleFileMeta は未読判定に使うファイルバージョン情報を返す。
// ツリーと同じ include/exclude を適用し、指定 worktree があればその worktree の実体から取得する。
func (s *Server) handleFileMeta(c echo.Context) error {
	rootName := c.QueryParam("root")
	ctx, err := s.rootByName(rootName)
	if err != nil {
		return err
	}

	baseDir, err := s.resolveWorktreeBaseForCtx(ctx, c.QueryParam("worktree"))
	if err != nil {
		return err
	}

	files, err := tree.GitFiles(baseDir)
	if err != nil {
		files, err = buildFileListFallback(baseDir)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "ファイル一覧の取得に失敗しました")
		}
	}

	opts := tree.BuildOptions{
		Include:  s.include,
		Exclude:  s.exclude,
		SortDesc: s.sortDescByRootName(rootName),
	}
	treeData := tree.BuildFromGitFiles(filepath.Base(ctx.basePath), files, opts)
	paths := collectTreeFilePaths(treeData)

	metas := make([]fileMeta, 0, len(paths))
	for _, p := range paths {
		info, statErr := os.Stat(filepath.Join(baseDir, filepath.FromSlash(p)))
		if statErr != nil || info.IsDir() {
			continue
		}
		metas = append(metas, fileMeta{
			Path:         p,
			ModifiedAtMs: info.ModTime().UnixMilli(),
			Size:         info.Size(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"root":     rootName,
		"worktree": c.QueryParam("worktree"),
		"files":    metas,
	})
}

func collectTreeFilePaths(item *tree.TreeItem) []string {
	if item == nil {
		return nil
	}
	if !item.IsDir {
		return []string{item.Path}
	}
	var paths []string
	for _, child := range item.Children {
		paths = append(paths, collectTreeFilePaths(child)...)
	}
	return paths
}

// handleFile はドキュメントを返す
func (s *Server) handleFile(c echo.Context) error {
	rootName := c.QueryParam("root")
	ctx, err := s.rootByName(rootName)
	if err != nil {
		return err
	}
	path := c.QueryParam("path")
	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが指定されていません")
	}
	if err := validateRelativePath(path); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが不正です")
	}

	// worktreeパラメータがある場合は別のworktreeからファイルを取得
	worktreeName := c.QueryParam("worktree")
	if worktreeName != "" {
		return s.handleFileFromWorktree(c, ctx, path, worktreeName)
	}

	doc, err := ctx.renderer.Render(path, "")
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "ファイルが見つかりません")
		}
		if os.IsPermission(err) {
			return echo.NewHTTPError(http.StatusForbidden, "アクセスが拒否されました")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "ファイルの処理に失敗しました")
	}

	return c.JSON(http.StatusOK, doc)
}

// handleFileWrite は指定パスのファイルを保存する。
// クエリ `root=write` で書き込みルート配下に保存（新規作成も許可）。
// `root=read`（または未指定）の場合は閲覧ルート配下の既存ファイルのみ更新可能。
func (s *Server) handleFileWrite(c echo.Context) error {
	rootName := c.QueryParam("root")
	ctx, err := s.rootByName(rootName)
	if err != nil {
		return err
	}
	rawPath := c.QueryParam("path")
	if rawPath == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが指定されていません")
	}
	if err := validateRelativePath(rawPath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが不正です")
	}

	const maxBytes = 10 * 1024 * 1024
	body, err := io.ReadAll(http.MaxBytesReader(c.Response().Writer, c.Request().Body, maxBytes))
	if err != nil {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "リクエストが大きすぎます")
	}

	absBase, _ := filepath.Abs(ctx.basePath)
	absFile, _ := filepath.Abs(filepath.Join(ctx.basePath, rawPath))
	rel, relErr := filepath.Rel(absBase, absFile)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return echo.NewHTTPError(http.StatusForbidden, "アクセスが拒否されました")
	}

	allowCreate := rootName == "write" || strings.HasPrefix(rootName, "write-")
	info, statErr := os.Stat(absFile)
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			return echo.NewHTTPError(http.StatusInternalServerError, "ファイル情報の取得に失敗しました")
		}
		if !allowCreate {
			return echo.NewHTTPError(http.StatusNotFound, "ファイルが見つかりません")
		}
		// 新規作成: 親ディレクトリを必要に応じて作成
		if mkErr := os.MkdirAll(filepath.Dir(absFile), 0o755); mkErr != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "ディレクトリ作成に失敗しました")
		}
	} else if info.IsDir() {
		return echo.NewHTTPError(http.StatusBadRequest, "ディレクトリは編集できません")
	}

	mode := os.FileMode(0o644)
	if info != nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(absFile, body, mode); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "ファイルの書き込みに失敗しました")
	}

	newInfo, err := os.Stat(absFile)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "保存後の情報取得に失敗しました")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"path":       rawPath,
		"modifiedAt": newInfo.ModTime().Unix(),
	})
}

// handleFileFromWorktree は別のworktreeからファイルを取得する
func (s *Server) handleFileFromWorktree(c echo.Context, ctx *rootCtx, path, worktreeName string) error {
	worktrees, err := worktree.List(ctx.gitRoot)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "worktreeの取得に失敗しました")
	}

	wt := worktree.FindByName(worktrees, worktreeName)
	if wt == nil {
		return echo.NewHTTPError(http.StatusNotFound, "worktreeが見つかりません")
	}

	// 現在のworktreeの場合は通常の処理
	if wt.Current {
		doc, err := ctx.renderer.Render(path, worktreeName)
		if err != nil {
			if os.IsNotExist(err) {
				return echo.NewHTTPError(http.StatusNotFound, "ファイルが見つかりません")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "ファイルの処理に失敗しました")
		}
		return c.JSON(http.StatusOK, doc)
	}

	wtBase := ctx.worktreeBaseDir(wt)
	wtRenderer := document.NewRenderer(wtBase)
	doc, err := wtRenderer.Render(path, worktreeName)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "ファイルが見つかりません")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "ファイルの処理に失敗しました")
	}

	return c.JSON(http.StatusOK, doc)
}

// handleRaw は資産ファイル（画像など）／ Markdown ソースを生バイトで配信する。
// クエリ `root=write` で書き込みルートから読み取る。
func (s *Server) handleRaw(c echo.Context) error {
	rootName := c.QueryParam("root")
	ctx, err := s.rootByName(rootName)
	if err != nil {
		return err
	}
	rawPath := c.QueryParam("path")
	if rawPath == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが指定されていません")
	}
	if err := validateRelativePath(rawPath); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが不正です")
	}

	baseDir, err := s.resolveWorktreeBaseForCtx(ctx, c.QueryParam("worktree"))
	if err != nil {
		return err
	}

	// パストラバーサル防御（resolveWorktreeBase 後に再確認）
	absBase, _ := filepath.Abs(baseDir)
	absFile, _ := filepath.Abs(filepath.Join(baseDir, rawPath))
	rel, relErr := filepath.Rel(absBase, absFile)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return echo.NewHTTPError(http.StatusForbidden, "アクセスが拒否されました")
	}

	info, statErr := os.Stat(absFile)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return echo.NewHTTPError(http.StatusNotFound, "ファイルが見つかりません")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "ファイルの読み込みに失敗しました")
	}
	if info.IsDir() {
		return echo.NewHTTPError(http.StatusBadRequest, "ディレクトリは配信できません")
	}

	// echo の c.File は拡張子から Content-Type を判定するが、
	// 未知の拡張子のために mime パッケージで保険をかける。
	if mime.TypeByExtension(filepath.Ext(absFile)) == "" {
		c.Response().Header().Set(echo.HeaderContentType, "application/octet-stream")
	}
	return c.File(absFile)
}

// resolveWorktreeBaseForCtx は指定 rootCtx で worktree 名から実ベースディレクトリを返す。
// 空文字なら ctx.basePath を返す。
func (s *Server) resolveWorktreeBaseForCtx(ctx *rootCtx, worktreeName string) (string, error) {
	if worktreeName == "" {
		return ctx.basePath, nil
	}
	worktrees, err := worktree.List(ctx.gitRoot)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusInternalServerError, "worktreeの取得に失敗しました")
	}
	wt := worktree.FindByName(worktrees, worktreeName)
	if wt == nil {
		return "", echo.NewHTTPError(http.StatusNotFound, "worktreeが見つかりません")
	}
	if wt.Current {
		return ctx.basePath, nil
	}
	return ctx.worktreeBaseDir(wt), nil
}

// handleWorktrees はworktree一覧を返す。クエリ `root=write` で書き込みルートに対して列挙。
func (s *Server) handleWorktrees(c echo.Context) error {
	ctx, err := s.rootByName(c.QueryParam("root"))
	if err != nil {
		return err
	}
	path := c.QueryParam("path")

	var worktrees []worktree.Worktree

	if path != "" {
		if err := validateRelativePath(path); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "パスが不正です")
		}
		worktrees, err = worktree.ListWithHashes(ctx.gitRoot, ctx.relSubdir, path)
	} else {
		worktrees, err = worktree.List(ctx.gitRoot)
	}

	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "worktreeの取得に失敗しました")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"worktrees": worktrees,
	})
}

// handleDiff はmainブランチとの差分を返す
func (s *Server) handleDiff(c echo.Context) error {
	rootName := c.QueryParam("root")
	ctx, err := s.rootByName(rootName)
	if err != nil {
		return err
	}
	path := c.QueryParam("path")
	if path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが指定されていません")
	}
	if err := validateRelativePath(path); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "パスが不正です")
	}

	// worktree一覧を取得（git root から）
	worktrees, err := worktree.List(ctx.gitRoot)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "worktreeの取得に失敗しました")
	}

	// mainブランチのworktreeを検索
	mainWt := worktree.FindByName(worktrees, "main")
	if mainWt == nil {
		return echo.NewHTTPError(http.StatusNotFound, "mainブランチが見つかりません")
	}

	// 現在のworktreeを検索
	var currentWt *worktree.Worktree
	for i := range worktrees {
		if worktrees[i].Current {
			currentWt = &worktrees[i]
			break
		}
	}
	if currentWt == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "現在のworktreeが見つかりません")
	}

	// mainと同じ場合は差分なし
	if currentWt.Name == "main" {
		return c.JSON(http.StatusOK, &diff.DiffResult{
			HasDiff:         false,
			BaseWorktree:    "main",
			CurrentWorktree: "main",
			Lines:           []diff.DiffLine{},
		})
	}

	mainFilePath := filepath.Join(ctx.worktreeBaseDir(mainWt), path)
	currentFilePath := filepath.Join(ctx.worktreeBaseDir(currentWt), path)

	// ファイル内容を読み込み
	mainContent, err := os.ReadFile(mainFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// mainにファイルがない場合は全て追加とみなす
			mainContent = []byte{}
		} else {
			return echo.NewHTTPError(http.StatusInternalServerError, "mainのファイル読み込みに失敗しました")
		}
	}

	currentContent, err := os.ReadFile(currentFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "現在のファイルが見つかりません")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "ファイルの読み込みに失敗しました")
	}

	if isDiffTooLarge(mainContent, currentContent) {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "ファイルが大きすぎるため差分を表示できません")
	}

	// 差分を計算
	result := diff.ComputeDiff(string(mainContent), string(currentContent), "main", currentWt.Name)

	return c.JSON(http.StatusOK, result)
}

// StartWithListener は指定された Listener でサーバーを開始する
func (s *Server) StartWithListener(listener net.Listener) error {
	s.echo.Listener = listener
	return s.echo.Start(listener.Addr().String())
}

// resolveAbs は絶対パス化＋シンボリックリンク解決を行う。
// macOS の /tmp → /private/tmp のような違いで relSubdir 計算が壊れるのを防ぐ。
func resolveAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func validateRelativePath(requestPath string) error {
	clean := filepath.Clean(requestPath)
	if clean == "." {
		return os.ErrInvalid
	}
	if filepath.IsAbs(clean) {
		return os.ErrInvalid
	}
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return os.ErrPermission
	}
	return nil
}

func isDiffTooLarge(mainContent, currentContent []byte) bool {
	const maxBytes = 2 * 1024 * 1024
	if len(mainContent)+len(currentContent) > maxBytes {
		return true
	}
	if countLines(mainContent)+countLines(currentContent) > 40000 {
		return true
	}
	return false
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := 1
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count
}
