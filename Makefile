BIN     := bin/einvoice-mandates
RELEASE ?= $(shell date -u +%Y.%m)
DIST    := dist

.PHONY: all build test lint validate dist examples clean

all: lint test build

build:
	go build -ldflags "-X main.version=$(RELEASE)" -o $(BIN) ./cmd/einvoice-mandates

test:
	go test ./...

lint:
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...

# Lint the dataset itself, failing on warnings as well as errors.
validate: build
	$(BIN) validate -strict

# Cut a release artefact and its static page into dist/.
dist: validate
	mkdir -p $(DIST)
	$(BIN) build -release $(RELEASE) -allow-draft -o $(DIST)/release-$(RELEASE).json
	$(BIN) render $(DIST)/release-$(RELEASE).json -o $(DIST)/index.html

# Regenerate the committed example artefact from the current source tree.
# The earlier example is a frozen historical snapshot and is never regenerated.
examples: build
	$(BIN) build -release 2026.07 -allow-draft -o examples/release-2026.07.json

clean:
	rm -rf bin $(DIST)
