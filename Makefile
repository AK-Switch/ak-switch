.PHONY: build clean lint fmt check help test-unit test-integration test-e2e test-all release

# build — compile the akswitch binary
build:
	go build -o bin/akswitch ./cmd/akswitch/

# clean — remove build artifacts
clean:
	rm -rf bin/ tmp/ *.test

# lint — run golangci-lint
lint:
	golangci-lint run ./...

# fmt — format Go source files
fmt:
	go fmt ./...

# check — run lint and vet
check: lint fmt

# help — show available targets
help:
	@awk '/^# /{desc=$$0; sub(/^# /,"",desc); next} /^[a-zA-Z_-]+:/{printf "  %-20s %s\n", $$1, desc}' $(MAKEFILE_LIST)

test-unit:
	go test -tags=unit -count=1 -short ./internal/...

test-integration:
	go test -tags=integration -count=1 -race ./test/integration/

test-e2e:
	go test -tags=e2e -count=1 -timeout=5m -race ./test/integration/

test-all: test-unit test-integration test-e2e

release:
	git tag $(VERSION) && git push origin $(VERSION)