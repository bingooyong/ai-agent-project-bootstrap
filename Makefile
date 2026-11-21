GO ?= go
.PHONY: build test vet format check
build:
	mkdir -p build
	$(GO) build -trimpath -o build/ombp ./cmd/ombp
test:
	$(GO) test -race -count=1 ./...
vet:
	$(GO) vet ./...
format:
	@test -z "$$(gofmt -l cmd internal)"
check: format vet test
	python3 scripts/check_plan.py
