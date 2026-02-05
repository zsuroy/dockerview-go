# Makefile

BINARY_NAME=dockerview
BUILD_DIR=./build
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"

# Go 参数
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

.PHONY: all
all: clean build

.PHONY: build
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

.PHONY: install
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o $(GOPATH)/bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)
	@echo "Install complete: $(GOPATH)/bin/$(BINARY_NAME)"

.PHONY: test
test:
	$(GOTEST) -v -race -cover ./...

.PHONY: clean
clean:
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@echo "Clean complete"

.PHONY: fmt
fmt:
	$(GOCMD) fmt ./...

.PHONY: lint
lint:
	$(GOCMD) vet ./...

.PHONY: deps
deps:
	$(GOMOD) download
	$(GOMOD) tidy

.PHONY: run
run:
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)
	$(BUILD_DIR)/$(BINARY_NAME)

# 跨平台构建
PLATFORMS=darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: release
release: clean
	@echo "Building release binaries..."
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		platform_split=($${platform//\// }); \
		GOOS=$${platform_split[0]}; \
		GOARCH=$${platform_split[1]}; \
		output_name=$(BINARY_NAME)-$${GOOS}-$${GOARCH}; \
		if [ $${GOOS} = "windows" ]; then \
			output_name+=.exe; \
		fi; \
		echo "Building $${output_name}..."; \
		GOOS=$${GOOS} GOARCH=$${GOARCH} $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$${output_name} ./cmd/$(BINARY_NAME); \
	done
	@echo "Release complete"
