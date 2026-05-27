package codingagent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	ModeAnnotationProofread = "annotation-proofread"
	SessionsDirName         = ".reader/coding-agent-sessions"
)

type RunRequest struct {
	Root        string `json:"root"`
	Path        string `json:"path"`
	Instruction string `json:"instruction"`
	Mode        string `json:"mode"`
}

type RunResponse struct {
	SessionID      string    `json:"sessionId"`
	CodexSessionID string    `json:"codexSessionId,omitempty"`
	Status         string    `json:"status"`
	Response       string    `json:"response"`
	ExitCode       int       `json:"exitCode"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Session struct {
	ID             string    `json:"id"`
	CodexSessionID string    `json:"codexSessionId,omitempty"`
	Mode           string    `json:"mode"`
	Root           string    `json:"root"`
	Path           string    `json:"path"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Turns          []Turn    `json:"turns"`
}

type Turn struct {
	Input      string    `json:"input"`
	Response   string    `json:"response"`
	ExitCode   int       `json:"exitCode"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Stdout     string    `json:"stdout,omitempty"`
	Stderr     string    `json:"stderr,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type SessionSummary struct {
	ID           string    `json:"id"`
	Mode         string    `json:"mode"`
	Root         string    `json:"root"`
	Path         string    `json:"path"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	ExitCode     int       `json:"exitCode"`
	LastResponse string    `json:"lastResponse,omitempty"`
}

type Runner interface {
	Run(ctx context.Context, args []string, stdin string) (RunResult, error)
}

type RunResult struct {
	Stdout      string
	Stderr      string
	LastMessage string
	SessionID   string
	ExitCode    int
}

type CodexRunner struct{}

func (r CodexRunner) Run(ctx context.Context, args []string, stdin string) (RunResult, error) {
	tmp, err := os.CreateTemp("", "reader-coding-agent-last-*.txt")
	if err != nil {
		return RunResult{}, err
	}
	outputPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(outputPath)

	cmd := exec.CommandContext(ctx, "codex", appendOutputArg(args, outputPath)...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	result := RunResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
	}
	if data, readErr := os.ReadFile(outputPath); readErr == nil {
		result.LastMessage = strings.TrimSpace(string(data))
	}
	result.SessionID = extractSessionID(result.Stdout)
	if result.SessionID == "" {
		result.SessionID = extractSessionID(result.Stderr)
	}
	return result, err
}

type Service struct {
	ProjectRoot string
	Runner      Runner
}

func NewService(projectRoot string, runner Runner) *Service {
	if runner == nil {
		runner = CodexRunner{}
	}
	return &Service{ProjectRoot: projectRoot, Runner: runner}
}

func (s *Service) Run(ctx context.Context, req RunRequest, fileContent string) (*Session, error) {
	if req.Mode == "" {
		req.Mode = ModeAnnotationProofread
	}
	if req.Mode != ModeAnnotationProofread {
		return nil, fmt.Errorf("unsupported coding agent mode: %s", req.Mode)
	}
	started := time.Now().UTC()
	args := []string{"exec", "--json", "-C", s.ProjectRoot, "--sandbox", "workspace-write", "-"}
	result, runErr := s.Runner.Run(ctx, args, BuildPrompt(req, fileContent))
	finished := time.Now().UTC()
	turn := Turn{
		Input:      req.Instruction,
		Response:   result.LastMessage,
		ExitCode:   result.ExitCode,
		StartedAt:  started,
		FinishedAt: finished,
		Stdout:     trimForStorage(result.Stdout),
		Stderr:     trimForStorage(result.Stderr),
	}
	if runErr != nil {
		turn.Error = runErr.Error()
	}
	session := &Session{
		ID:             newID("session"),
		CodexSessionID: result.SessionID,
		Mode:           req.Mode,
		Root:           req.Root,
		Path:           filepath.ToSlash(req.Path),
		CreatedAt:      started,
		UpdatedAt:      finished,
		Turns:          []Turn{turn},
	}
	if saveErr := s.SaveSession(session); saveErr != nil && runErr == nil {
		runErr = saveErr
	}
	return session, runErr
}

func BuildPrompt(req RunRequest, fileContent string) string {
	instruction := strings.TrimSpace(req.Instruction)
	if instruction == "" {
		instruction = "アノテーションに従って本文を添削してください。"
	}
	var b strings.Builder
	b.WriteString("You are a coding agent invoked from reader.\n")
	b.WriteString("Mode: annotation-proofread.\n")
	b.WriteString("Task: proofread and revise the selected writing file according to existing annotations in the file.\n")
	b.WriteString("If annotations use lines such as `@@instruction` or `@@ instruction`, treat them as user instructions for the nearby text.\n")
	b.WriteString("Edit the target file only when needed. Do not change unrelated files.\n")
	b.WriteString("Preserve the author's intent and avoid speculative rewrites.\n\n")
	fmt.Fprintf(&b, "Target root: %s\n", req.Root)
	fmt.Fprintf(&b, "Target path: %s\n\n", filepath.ToSlash(req.Path))
	b.WriteString("<instruction>\n")
	b.WriteString(instruction)
	b.WriteString("\n</instruction>\n\n")
	b.WriteString("<target_file>\n")
	b.WriteString(fileContent)
	b.WriteString("\n</target_file>\n")
	return b.String()
}

func (s *Service) SaveSession(session *Session) error {
	dir := filepath.Join(s.ProjectRoot, SessionsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, session.ID+".json"), data, 0o644)
}

func (s *Service) ReadSession(id string) (*Session, error) {
	if !IsValidID(id) {
		return nil, os.ErrInvalid
	}
	data, err := os.ReadFile(filepath.Join(s.ProjectRoot, SessionsDirName, id+".json"))
	if err != nil {
		return nil, err
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *Service) ListSessions() ([]SessionSummary, error) {
	dir := filepath.Join(s.ProjectRoot, SessionsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionSummary{}, nil
		}
		return nil, err
	}
	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		session, err := s.ReadSession(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			continue
		}
		summary := SessionSummary{
			ID:        session.ID,
			Mode:      session.Mode,
			Root:      session.Root,
			Path:      session.Path,
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		}
		if len(session.Turns) > 0 {
			last := session.Turns[len(session.Turns)-1]
			summary.ExitCode = last.ExitCode
			summary.LastResponse = last.Response
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func RunResponseFromSession(session *Session) RunResponse {
	response := RunResponse{
		SessionID:      session.ID,
		CodexSessionID: session.CodexSessionID,
		Status:         "completed",
		UpdatedAt:      session.UpdatedAt,
	}
	if len(session.Turns) > 0 {
		last := session.Turns[len(session.Turns)-1]
		response.Response = last.Response
		response.ExitCode = last.ExitCode
		if last.Error != "" || last.ExitCode != 0 {
			response.Status = "failed"
		}
	}
	return response
}

func IsValidID(id string) bool {
	return regexp.MustCompile(`^[a-z]+-[a-f0-9]{16}$`).MatchString(id)
}

func appendOutputArg(args []string, outputPath string) []string {
	fullArgs := append([]string{}, args...)
	if len(fullArgs) > 0 && fullArgs[len(fullArgs)-1] == "-" {
		fullArgs = fullArgs[:len(fullArgs)-1]
		return append(fullArgs, "-o", outputPath, "-")
	}
	return append(fullArgs, "-o", outputPath)
}

func extractSessionID(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(scanner.Text()), &event); err != nil {
			continue
		}
		if id := findStringKey(event, "session_id", "sessionId", "thread_id", "threadId"); id != "" {
			return id
		}
	}
	return ""
}

func findStringKey(value interface{}, keys ...string) string {
	m, ok := value.(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range keys {
		if v, ok := m[key].(string); ok && v != "" {
			return v
		}
	}
	for _, v := range m {
		if id := findStringKey(v, keys...); id != "" {
			return id
		}
	}
	return ""
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func trimForStorage(value string) string {
	const max = 20000
	if len(value) <= max {
		return value
	}
	return value[:max] + "\n...[truncated]"
}
