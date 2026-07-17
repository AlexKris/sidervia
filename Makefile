.PHONY: build check test test-race web-install web-generate web-check web-build clean

build: web-build
	go build -trimpath -o bin/sidervia ./cmd/sidervia

check: test web-check
	@test -z "$$(gofmt -l $$(find cmd internal migrations -name '*.go' -type f))"
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

web-install:
	pnpm --dir web install --frozen-lockfile

web-generate:
	pnpm --dir web generate:api

web-check:
	pnpm --dir web lint
	pnpm --dir web typecheck
	pnpm --dir web test --run
	pnpm --dir web build

web-build:
	pnpm --dir web build

clean:
	rm -rf bin web/dist/assets web/dist/index.html
