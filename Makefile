.PHONY: test test-unit test-integration test-global test-global-integration test-all clean-testcache help

help:
	@echo "Available targets:"
	@echo "  make test                  - Run all tests (unit + integration, optimized timeouts)"
	@echo "  make test-unit             - Run only unit tests (fast, skips integration)"
	@echo "  make test-integration      - Run only integration tests"
	@echo "  make test-global           - Run only global tests (non-integration)"
	@echo "  make test-global-integration - Run only global integration tests"
	@echo "  make test-all              - Alias for 'test'"
	@echo "  make coverage              - Generate HTML coverage report"
	@echo "  make coverage-report       - Print coverage summary"
	@echo "  make clean-testcache       - Clean go test cache"

clean-testcache:
	@go clean -testcache

test: clean-testcache
	go test -race -count=1 -v ./...

test-unit: clean-testcache
	go test -race -count=1 -short -v ./...

test-integration: clean-testcache
	go test -race -count=1 -v -run='.*Integration$$' ./...

test-global: clean-testcache
	go test -race -count=1 -v ./global_telemetry_test.go ./telemetry.go ./config.go
	go test -race -count=1 -v ./logger/global_test.go ./logger/logger.go ./logger/config.go ./logger/global.go ./logger/file_writer.go
	go test -race -count=1 -v ./meter/global_test.go ./meter/meter.go ./meter/config.go ./meter/global.go ./meter/runtime.go
	go test -race -count=1 -v ./tracer/global_test.go ./tracer/tracer.go ./tracer/config.go ./tracer/global.go
	go test -race -count=1 -v ./profiler/global_test.go ./profiler/profiler.go ./profiler/config.go ./profiler/global.go ./profiler/trace_link.go

test-global-integration: clean-testcache
	go test -race -count=1 -v -run='TestGlobal.*Integration$$' ./...

test-all: test

coverage: clean-testcache
	go test -race -count=1 -v -cover -coverprofile=coverage.out ./...
	@grep -v "_test_helpers_test.go" coverage.out > coverage_filtered.out || true
	@grep -v "internal/testutil/" coverage_filtered.out > coverage.out || true
	@rm -f coverage_filtered.out

coverage-report: coverage
	go tool cover -func=coverage.out | tail -n1
