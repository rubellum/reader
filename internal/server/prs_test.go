package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGHRunner struct {
	mu      sync.Mutex
	calls   int
	outputs map[string]string
	err     error
}

func (r *fakeGHRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	search := ""
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--search" {
			search = args[i+1]
			break
		}
	}
	out := r.outputs[search]
	if out == "" {
		out = "[]"
	}
	return []byte(out), r.err
}

func (r *fakeGHRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestClassifyPullRequest(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		pr         ghPullRequest
		wantOK     bool
		wantReason string
	}{
		{
			name:   "review request",
			source: "review_requested",
			pr: ghPullRequest{
				Number:    1,
				Title:     "review me",
				URL:       "https://github.com/o/r/pull/1",
				UpdatedAt: "2026-05-26T10:00:00Z",
			},
			wantOK:     true,
			wantReason: "review_requested",
		},
		{
			name:   "draft review request is ignored",
			source: "review_requested",
			pr: ghPullRequest{
				Number:  2,
				URL:     "https://github.com/o/r/pull/2",
				IsDraft: true,
			},
			wantOK: false,
		},
		{
			name:   "author changes requested",
			source: "author",
			pr: ghPullRequest{
				Number:         3,
				URL:            "https://github.com/o/r/pull/3",
				ReviewDecision: "CHANGES_REQUESTED",
			},
			wantOK:     true,
			wantReason: "changes_requested",
		},
		{
			name:   "author checks failed",
			source: "author",
			pr: ghPullRequest{
				Number:            4,
				URL:               "https://github.com/o/r/pull/4",
				StatusCheckRollup: json.RawMessage(`{"state":"FAILURE"}`),
			},
			wantOK:     true,
			wantReason: "checks_failed",
		},
		{
			name:   "author already done",
			source: "author",
			pr: ghPullRequest{
				Number:         5,
				URL:            "https://github.com/o/r/pull/5",
				ReviewDecision: "APPROVED",
			},
			wantOK: false,
		},
		{
			name:   "author unknown merge state is ignored",
			source: "author",
			pr: ghPullRequest{
				Number:           6,
				URL:              "https://github.com/o/r/pull/6",
				MergeStateStatus: "UNKNOWN",
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := classifyPullRequest(tt.pr, tt.source)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestPullRequestVersionSizeIncludesActionableState(t *testing.T) {
	base := ghPullRequest{
		Number:            1,
		URL:               "https://github.com/o/r/pull/1",
		UpdatedAt:         "2026-05-26T10:00:00Z",
		StatusCheckRollup: json.RawMessage(`{"state":"SUCCESS"}`),
	}
	changed := base
	changed.StatusCheckRollup = json.RawMessage(`{"state":"FAILURE"}`)

	baseItem, ok := classifyPullRequest(base, "assigned")
	if !ok {
		t.Fatalf("expected base pull request to be classified")
	}
	changedItem, ok := classifyPullRequest(changed, "assigned")
	if !ok {
		t.Fatalf("expected changed pull request to be classified")
	}
	if baseItem.VersionMs != changedItem.VersionMs {
		t.Fatalf("VersionMs should stay tied to updatedAt: %d != %d", baseItem.VersionMs, changedItem.VersionMs)
	}
	if baseItem.VersionSize == changedItem.VersionSize {
		t.Fatalf("VersionSize should change when actionable state changes")
	}
}

func TestHandlePullRequestsUsesOneMinuteCache(t *testing.T) {
	srv := newTestServer(t.TempDir(), nil, nil, false)
	runner := &fakeGHRunner{outputs: map[string]string{
		"review-requested:@me": `[{"number":1,"title":"Review","url":"https://github.com/o/r/pull/1","author":{"login":"alice"},"updatedAt":"2026-05-26T10:00:00Z"}]`,
		"assignee:@me":         `[]`,
		"author:@me":           `[]`,
	}}
	srv.prRunner = runner
	ts := httptest.NewServer(srv.echo)
	t.Cleanup(ts.Close)

	for i := 0; i < 2; i++ {
		resp := mustGet(t, ts.URL+"/api/pull-requests")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var payload pullRequestResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp.Body.Close()
		if len(payload.Items) != 1 {
			t.Fatalf("items = %d, want 1", len(payload.Items))
		}
		if i == 1 && !payload.Cached {
			t.Fatalf("second response should be cached")
		}
	}

	if got := runner.callCount(); got != 3 {
		t.Fatalf("gh call count = %d, want 3", got)
	}
}

func TestCachedPullRequestsReturnsStaleDataOnFailure(t *testing.T) {
	srv := newTestServer(t.TempDir(), nil, nil, false)
	runner := &fakeGHRunner{outputs: map[string]string{
		"review-requested:@me": `[{"number":1,"title":"Review","url":"https://github.com/o/r/pull/1","updatedAt":"2026-05-26T10:00:00Z"}]`,
	}}
	srv.prRunner = runner

	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
	first := srv.cachedPullRequests(context.Background(), now)
	if len(first.Items) != 1 || first.ErrorMessage != "" {
		t.Fatalf("first response = %+v", first)
	}

	runner.err = context.Canceled
	runner.outputs = map[string]string{
		"review-requested:@me": "network unavailable",
	}
	second := srv.cachedPullRequests(context.Background(), now.Add(2*time.Minute))
	if len(second.Items) != 1 {
		t.Fatalf("stale items = %d, want 1", len(second.Items))
	}
	if !second.Stale {
		t.Fatalf("expected stale response")
	}
	if !strings.Contains(second.ErrorMessage, "gh pull request list failed") {
		t.Fatalf("error message = %q", second.ErrorMessage)
	}
}
