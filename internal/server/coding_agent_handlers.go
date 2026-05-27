package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rubellum/reader/internal/codingagent"
)

func (s *Server) handleCodingAgentRun(c echo.Context) error {
	if s.codingAgentService == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "coding agent service is not available")
	}
	var req codingagent.RunRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "リクエストの解析に失敗しました")
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = codingagent.ModeAnnotationProofread
	}
	if req.Mode != codingagent.ModeAnnotationProofread {
		return echo.NewHTTPError(http.StatusBadRequest, "未対応の coding agent mode です")
	}
	if strings.TrimSpace(req.Instruction) == "" {
		req.Instruction = "アノテーションに従って本文を添削してください。"
	}

	content, err := s.readCodingAgentTarget(req.Root, req.Path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Minute)
	defer cancel()
	session, runErr := s.codingAgentService.Run(ctx, req, content)
	if session == nil {
		return echo.NewHTTPError(http.StatusBadRequest, runErr.Error())
	}
	status := http.StatusOK
	if runErr != nil {
		status = http.StatusInternalServerError
	}
	return c.JSON(status, codingagent.RunResponseFromSession(session))
}

func (s *Server) handleCodingAgentSessions(c echo.Context) error {
	if s.codingAgentService == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "coding agent service is not available")
	}
	sessions, err := s.codingAgentService.ListSessions()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "セッション一覧の取得に失敗しました")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"sessions": sessions})
}

func (s *Server) handleCodingAgentSession(c echo.Context) error {
	if s.codingAgentService == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "coding agent service is not available")
	}
	session, err := s.codingAgentService.ReadSession(c.Param("sessionID"))
	if err != nil {
		if errors.Is(err, os.ErrInvalid) {
			return echo.NewHTTPError(http.StatusBadRequest, "セッションIDが不正です")
		}
		if os.IsNotExist(err) {
			return echo.NewHTTPError(http.StatusNotFound, "セッションが見つかりません")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "セッションの取得に失敗しました")
	}
	return c.JSON(http.StatusOK, session)
}

func (s *Server) readCodingAgentTarget(rootName, rawPath string) (string, error) {
	if rawPath == "" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "パスが指定されていません")
	}
	if err := validateRelativePath(rawPath); err != nil {
		return "", echo.NewHTTPError(http.StatusBadRequest, "パスが不正です")
	}
	ctx, err := s.writeRootByName(rootName)
	if err != nil {
		return "", err
	}
	absBase, _ := filepath.Abs(ctx.basePath)
	absFile, _ := filepath.Abs(filepath.Join(ctx.basePath, rawPath))
	rel, relErr := filepath.Rel(absBase, absFile)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", echo.NewHTTPError(http.StatusForbidden, "対象ファイルはroot配下に限定されます")
	}
	info, err := os.Stat(absFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", echo.NewHTTPError(http.StatusNotFound, "対象ファイルが見つかりません")
		}
		return "", echo.NewHTTPError(http.StatusInternalServerError, "対象ファイルの確認に失敗しました")
	}
	if info.IsDir() {
		return "", echo.NewHTTPError(http.StatusBadRequest, "ディレクトリは対象にできません")
	}
	resolvedBase, err := filepath.EvalSymlinks(absBase)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusInternalServerError, "rootの確認に失敗しました")
	}
	resolvedFile, err := filepath.EvalSymlinks(absFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", echo.NewHTTPError(http.StatusNotFound, "対象ファイルが見つかりません")
		}
		return "", echo.NewHTTPError(http.StatusForbidden, "対象ファイルはroot配下に限定されます")
	}
	relResolved, relResolvedErr := filepath.Rel(resolvedBase, resolvedFile)
	if relResolvedErr != nil || relResolved == ".." || strings.HasPrefix(relResolved, ".."+string(filepath.Separator)) {
		return "", echo.NewHTTPError(http.StatusForbidden, "対象ファイルはroot配下に限定されます")
	}
	if info.Size() > 1024*1024 {
		return "", echo.NewHTTPError(http.StatusRequestEntityTooLarge, "対象ファイルが大きすぎます")
	}
	data, err := os.ReadFile(resolvedFile)
	if err != nil {
		return "", echo.NewHTTPError(http.StatusInternalServerError, "対象ファイルの読み込みに失敗しました")
	}
	return string(data), nil
}

func (s *Server) writeRootByName(rootName string) (*rootCtx, error) {
	for i := range s.writeRoots {
		if s.writeRoots[i].id == rootName {
			return &s.writeRoots[i].ctx, nil
		}
	}
	return nil, echo.NewHTTPError(http.StatusBadRequest, "書き込みルートが設定されていません")
}
