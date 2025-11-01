.PHONY: test test-unit test-integration test-global test-global-integration test-all clean-cache help

help:
	@echo "Available targets:"
	@echo "  make test                  - Run all tests (unit + global + integration)"
	@echo "  make test-unit             - Run only unit tests (non-global, non-integration)"
	@echo "  make test-global           - Run only global tests (non-integration)"
	@echo "  make test-integration      - Run only integration tests (non-global)"
	@echo "  make test-global-integration - Run only global integration tests"
	@echo "  make test-all              - Run all tests with coverage"
	@echo "  make coverage              - Generate HTML coverage report"
	@echo "  make coverage-report       - Print coverage summary"
	@echo "  make clean-cache           - Clean go test cache"

clean-cache:
	@go clean -cache

test: clean-cache
	go test -race -count=1 -v ./...

test-unit: clean-cache
	go test -race -count=1 -short -v ./...

test-integration: clean-cache
	go test -race -count=1 -v -run='.*Integration$$' ./...

test-global: clean-cache
	go test -race -count=1 -v ./global_telemetry_test.go ./telemetry.go ./config.go
	go test -race -count=1 -v ./logger/global_test.go ./logger/logger.go ./logger/config.go ./logger/global.go ./logger/file_writer.go
	go test -race -count=1 -v ./meter/global_test.go ./meter/meter.go ./meter/config.go ./meter/global.go ./meter/runtime.go
	go test -race -count=1 -v ./tracer/global_test.go ./tracer/tracer.go ./tracer/config.go ./tracer/global.go
	go test -race -count=1 -v ./profiler/global_test.go ./profiler/profiler.go ./profiler/config.go ./profiler/global.go ./profiler/trace_link.go

test-global-integration: clean-cache
	go test -race -count=1 -v -run='TestGlobal.*Integration$$' ./...

test-all: clean-cache
	go test -race -count=1 -v -cover -coverprofile=coverage.out ./...

coverage: test-all
	go tool cover -html=coverage.out

coverage-report: test-all
	go tool cover -func=coverage.out | tail -n1
