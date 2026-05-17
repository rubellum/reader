GOCACHE ?= /tmp/reader-go-build-cache
export GOCACHE

.PHONY: serve fmt lint build unit e2e test quality

serve:
	go run . -host 127.0.0.1

# リリース可能な品質を担保するための検証一式。
# - gofmt: コード整形が一致していること（差分があれば失敗）
# - go vet: 静的解析で疑わしいコードを検出
# - go build: 全パッケージがビルド可能
# - go test -race: 競合検出付きで全テスト（キャッシュ無効）
# - playwright-cli: 重要なサイドバー構成と編集ルート選択をブラウザで検証
fmt:
	@echo "==> gofmt check"
	@diff=$$(gofmt -l .); \
	if [ -n "$$diff" ]; then \
		echo "ERROR: 以下のファイルが gofmt 整形されていません:"; \
		echo "$$diff"; \
		exit 1; \
	fi

lint:
	@echo "==> go vet"
	@go vet ./...

build:
	@echo "==> go build"
	@go build ./...

unit:
	@echo "==> go test -race"
	@go test -race -count=1 ./...

e2e:
	@echo "==> playwright-cli e2e"
	@bash scripts/e2e-playwright-cli.sh

test: fmt lint build unit e2e

quality: test
