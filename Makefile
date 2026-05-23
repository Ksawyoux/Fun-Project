# Root Makefile — iterates the multi-module workspace.
# Each zone is its own Go module; targets here fan out and aggregate.

MODULES := nif zone2 zone3 zone4 zone5 zone6 cmd/archgraph

.PHONY: test vet build tidy clean help

help:
	@echo "Targets:"
	@echo "  test   — go test ./... in every module"
	@echo "  vet    — go vet ./... in every module"
	@echo "  build  — go build ./... in every module"
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

tidy:
	@set -e; for m in $(MODULES); do \
		echo "==> tidy $$m"; \
		(cd $$m && go mod tidy) || exit 1; \
	done

clean:
	rm -f zone6/archgraph cmd/archgraph/archgraph
	rm -rf zone2/zone2-state zone2-state
	rm -f zone4.db zone4.db-* zone3.db zone3.db-*
