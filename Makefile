.PHONY: test-unit test-integration test-e2e test-all release

test-unit:
	go test -tags=unit -count=1 -short ./internal/...

test-integration:
	go test -tags=integration -count=1 -race ./

test-e2e:
	go test -tags=e2e -count=1 -timeout=5m -race ./

test-all: test-unit test-integration test-e2e

release:
	git tag $(VERSION) && git push origin $(VERSION)