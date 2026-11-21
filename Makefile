GO ?= go
.PHONY: build test vet format schema check
build:
	mkdir -p build
	$(GO) build -trimpath -o build/ombp ./cmd/ombp
test:
	$(GO) test -race -count=1 ./...
vet:
	$(GO) vet ./...
format:
	@test -z "$$(gofmt -l cmd internal scripts/gen_schema.go)"
schema:
	$(GO) run scripts/gen_schema.go --check
check: format vet test schema
	python3 scripts/check_plan.py
