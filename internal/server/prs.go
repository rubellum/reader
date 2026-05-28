package server

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

const pullRequestCacheTTL = time.Minute

type pullRequestItem struct {
	ID          string `json:"id"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Author      string `json:"author"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	UpdatedAt   string `json:"updatedAt"`
	VersionMs   int64  `json:"versionMs"`
	VersionSize int64  `json:"versionSize"`
}

type pullRequestResponse struct {
	Enabled      bool              `json:"enabled"`
	Cached       bool              `json:"cached"`
	Stale        bool              `json:"stale,omitempty"`
	FetchedAt    string            `json:"fetchedAt,omitempty"`
	ExpiresAt    string            `json:"expiresAt,omitempty"`
	ErrorMessage string            `json:"errorMessage,omitempty"`
	Items        []pullRequestItem `json:"items"`
}

type ghRunner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

type defaultGHRunner struct{}

func (defaultGHRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

type pullRequestCache struct {
	mu           sync.Mutex
	cond         *sync.Cond
	inflight     bool
	fetchedAt    time.Time
	items        []pullRequestItem
	errorMessage string
}

func newPullRequestCache() *pullRequestCache {
	c := &pullRequestCache{}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (s *Server) handlePullRequests(c echo.Context) error {
	if !s.pullRequestsEnabled {
		return c.JSON(http.StatusOK, pullRequestResponse{
			Enabled: false,
			Items:   []pullRequestItem{},
		})
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	resp := s.cachedPullRequests(ctx, time.Now())
	return c.JSON(http.StatusOK, resp)
}

func (s *Server) cachedPullRequests(ctx context.Context, now time.Time) pullRequestResponse {
	cache := s.prCache

	cache.mu.Lock()
	for {
		if !cache.fetchedAt.IsZero() && now.Sub(cache.fetchedAt) < pullRequestCacheTTL {
			resp := cache.responseLocked(true, cache.errorMessage != "")
			cache.mu.Unlock()
			return resp
		}
		if !cache.inflight {
			cache.inflight = true
			break
		}
		cache.cond.Wait()
	}
	cache.mu.Unlock()

	items, err := s.fetchPullRequests(ctx)
	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}

	cache.mu.Lock()
	if err == nil {
		cache.items = items
	}
	cache.errorMessage = errorMessage
	cache.fetchedAt = now
	cache.inflight = false
	resp := cache.responseLocked(false, err != nil)
	cache.cond.Broadcast()
	cache.mu.Unlock()
	return resp
}

func (c *pullRequestCache) responseLocked(cached, stale bool) pullRequestResponse {
	items := append([]pullRequestItem(nil), c.items...)
	resp := pullRequestResponse{
		Enabled:      c.errorMessage == "" || len(items) > 0,
		Cached:       cached,
		Stale:        stale && len(items) > 0,
		ErrorMessage: c.errorMessage,
		Items:        items,
	}
	if !c.fetchedAt.IsZero() {
		resp.FetchedAt = c.fetchedAt.Format(time.RFC3339)
		resp.ExpiresAt = c.fetchedAt.Add(pullRequestCacheTTL).Format(time.RFC3339)
	}
	if resp.Enabled && resp.Items == nil {
		resp.Items = []pullRequestItem{}
	}
	return resp
}

func (s *Server) fetchPullRequests(ctx context.Context) ([]pullRequestItem, error) {
	if len(s.readRoots) == 0 {
		return nil, fmt.Errorf("閲覧ルートが設定されていません")
	}
	dir := s.readRoots[0].ctx.gitRoot
	runner := s.prRunner
	if runner == nil {
		runner = defaultGHRunner{}
	}

	queries := []struct {
		search string
		source string
	}{
		{search: "review-requested:@me", source: "review_requested"},
		{search: "assignee:@me", source: "assigned"},
		{search: "author:@me", source: "author"},
	}

	byID := map[string]pullRequestItem{}
	for _, q := range queries {
		out, err := runner.Run(ctx, dir, pullRequestListArgs(q.search)...)
		if err != nil {
			return nil, formatGHError(err, out)
		}
		prs, err := parseGHPullRequests(out)
		if err != nil {
			return nil, err
		}
		for _, pr := range prs {
			item, ok := classifyPullRequest(pr, q.source)
			if !ok {
				continue
			}
			if existing, exists := byID[item.ID]; exists {
				byID[item.ID] = mergePullRequestItems(existing, item)
			} else {
				byID[item.ID] = item
			}
		}
	}

	items := make([]pullRequestItem, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sortPullRequestItems(items)
	return items, nil
}

func pullRequestListArgs(search string) []string {
	return []string{
		"pr", "list",
		"--state", "open",
		"--limit", "100",
		"--search", search,
		"--json", "number,title,url,author,updatedAt,isDraft,reviewDecision,reviewRequests,statusCheckRollup,mergeStateStatus,labels",
	}
}

func formatGHError(err error, out []byte) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("gh pull request list failed: %s", msg)
}

type ghPullRequest struct {
	Number            int             `json:"number"`
	Title             string          `json:"title"`
	URL               string          `json:"url"`
	Author            ghUser          `json:"author"`
	UpdatedAt         string          `json:"updatedAt"`
	IsDraft           bool            `json:"isDraft"`
	ReviewDecision    string          `json:"reviewDecision"`
	ReviewRequests    json.RawMessage `json:"reviewRequests"`
	StatusCheckRollup json.RawMessage `json:"statusCheckRollup"`
	MergeStateStatus  string          `json:"mergeStateStatus"`
	Labels            []ghLabel       `json:"labels"`
}

type ghUser struct {
	Login string `json:"login"`
}

type ghLabel struct {
	Name string `json:"name"`
}

func parseGHPullRequests(out []byte) ([]ghPullRequest, error) {
	var prs []ghPullRequest
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("gh pull request JSON parse failed: %w", err)
	}
	return prs, nil
}

func classifyPullRequest(pr ghPullRequest, source string) (pullRequestItem, bool) {
	if pr.Number == 0 || pr.URL == "" {
		return pullRequestItem{}, false
	}
	if pr.IsDraft && source == "review_requested" {
		return pullRequestItem{}, false
	}

	reason, status := "", ""
	switch source {
	case "review_requested":
		reason, status = "review_requested", "review requested"
	case "assigned":
		reason, status = "assigned", "assigned"
	case "author":
		reason, status = authorPullRequestReason(pr)
		if reason == "" {
			return pullRequestItem{}, false
		}
	default:
		return pullRequestItem{}, false
	}

	updatedAt, versionMs := parsePRUpdatedAt(pr.UpdatedAt)
	versionSize := pullRequestStateHash(pr, reason, status)
	return pullRequestItem{
		ID:          pr.URL,
		Number:      pr.Number,
		Title:       pr.Title,
		URL:         pr.URL,
		Author:      pr.Author.Login,
		Reason:      reason,
		Status:      status,
		UpdatedAt:   updatedAt,
		VersionMs:   versionMs,
		VersionSize: versionSize,
	}, true
}

func authorPullRequestReason(pr ghPullRequest) (string, string) {
	if strings.EqualFold(pr.ReviewDecision, "CHANGES_REQUESTED") {
		return "changes_requested", "changes requested"
	}
	if hasFailingStatusCheck(pr.StatusCheckRollup) {
		return "checks_failed", "checks failed"
	}
	switch strings.ToUpper(pr.MergeStateStatus) {
	case "DIRTY", "BLOCKED":
		return "blocked", strings.ToLower(strings.ReplaceAll(pr.MergeStateStatus, "_", " "))
	default:
		return "", ""
	}
}

func pullRequestStateHash(pr ghPullRequest, reason, status string) int64 {
	h := fnv.New32a()
	parts := []string{
		reason,
		status,
		pr.ReviewDecision,
		pr.MergeStateStatus,
		string(pr.StatusCheckRollup),
		string(pr.ReviewRequests),
	}
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return int64(h.Sum32())
}

func hasFailingStatusCheck(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return hasFailingStatusValue(value)
}

func hasFailingStatusValue(value any) bool {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if hasFailingStatusValue(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			switch strings.ToLower(key) {
			case "state", "conclusion":
				status, ok := item.(string)
				if ok && isFailingStatus(status) {
					return true
				}
			default:
				if hasFailingStatusValue(item) {
					return true
				}
			}
		}
	}
	return false
}

func isFailingStatus(status string) bool {
	switch strings.ToUpper(status) {
	case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return true
	default:
		return false
	}
}

func parsePRUpdatedAt(raw string) (string, int64) {
	if raw == "" {
		return "", 0
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw, 0
	}
	return t.Format(time.RFC3339), t.UnixMilli()
}

func mergePullRequestItems(a, b pullRequestItem) pullRequestItem {
	if priorityForPRReason(b.Reason) < priorityForPRReason(a.Reason) {
		a.Reason = b.Reason
		a.Status = b.Status
	}
	if b.VersionMs > a.VersionMs {
		a.UpdatedAt = b.UpdatedAt
		a.VersionMs = b.VersionMs
	}
	if b.VersionSize != 0 {
		a.VersionSize = b.VersionSize
	}
	return a
}

func priorityForPRReason(reason string) int {
	switch reason {
	case "review_requested":
		return 0
	case "changes_requested":
		return 1
	case "checks_failed":
		return 2
	case "blocked":
		return 3
	case "assigned":
		return 4
	default:
		return 9
	}
}

func sortPullRequestItems(items []pullRequestItem) {
	for i := 1; i < len(items); i++ {
		item := items[i]
		j := i - 1
		for j >= 0 && pullRequestLess(item, items[j]) {
			items[j+1] = items[j]
			j--
		}
		items[j+1] = item
	}
}

func pullRequestLess(a, b pullRequestItem) bool {
	if a.VersionMs != b.VersionMs {
		return a.VersionMs > b.VersionMs
	}
	return a.Number > b.Number
}
