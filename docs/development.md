# Development

This document lists the public, reproducible checks and local verification harnesses used for reader development.

## Quality Checks

Run formatting and static analysis before opening a pull request:

```sh
make fmt
make lint
```

`make fmt` checks `gofmt` output. `make lint` runs `go vet ./...`.

## Tests

Run the Go test suite:

```sh
go test ./...
```

For the full release-quality check suite, use:

```sh
make test
```

## Local UI Verification

For changes that affect the read/write UI, start the app with write mode enabled:

```sh
go run . -no-open -port 3334 -write . .
```

Open:

```text
http://127.0.0.1:3334/
```

When verifying UI behavior, record:

- the URL tested
- the controls used
- expected result
- observed result
- concise evidence such as visible text, DOM state, or a screenshot

Do not include personal absolute paths, credentials, tokens, private URLs, or session-specific tool logs in pull requests.
