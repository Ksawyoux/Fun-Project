# Root Makefile — iterates the multi-module workspace.
# Each service is its own Go module; targets here fan out and aggregate.

MODULES := nif ingestion pipeline storage serving interfaces cmd/archgraph

.PHONY: test vet build check tidy clean help

help:
	@echo "Targets:"
	@echo "  test   — go test ./... in every module"
	@echo "  vet    — go vet ./... in every module"
	@echo "  build  — go build ./... in every module"
	@echo "  check  — test, vet, and build in every module"
	@echo "  tidy   — go mod tidy in every module"
	@echo "  clean  — remove built binaries and runtime state"

test:
	@set -e; for m in $(MODULES); do \
		echo "==> test $$m"; \
		(cd $$m && go test ./...) || exit 1; \
	done

vet:
	@set -e; for m in $(MODULES); do \
		echo "==> vet $$m"; \
		(cd $$m && go vet ./...) || exit 1; \
	done

build:
	@set -e; for m in $(MODULES); do \
		echo "==> build $$m"; \
		(cd $$m && go build ./...) || exit 1; \
	done

check: test vet build

tidy:
	@set -e; for m in $(MODULES); do \
		echo "==> tidy $$m"; \
		(cd $$m && go mod tidy) || exit 1; \
	done

clean:
	rm -f interfaces/archgraph-cli cmd/archgraph/archgraph
	rm -rf ingestion/ingestion-state ingestion-state
	rm -f storage.db storage.db-* pipeline.db pipeline.db-*
	rm -f storage/storage.db* pipeline/pipeline.db*
