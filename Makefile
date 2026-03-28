BINARY    := server
BUILD_DIR := ./bin
CMD       := ./cmd/server
REGISTRY  := variantplatformacr.azurecr.io
IMAGE     := variant-internal-mcp
TAG       ?= latest

# ── Local dev ──────────────────────────────────────────────────────────────────
.PHONY: run
run: ## Run server locally (HOST defaults to 0.0.0.0 via config, override with HOST=127.0.0.1)
	HOST=127.0.0.1 go run $(CMD)

.PHONY: dev
dev: ## Hot-reload with Air (go install github.com/air-verse/air@latest)
	air

.PHONY: inspect
inspect: ## Open MCP Inspector (server must already be running)
	npx @modelcontextprotocol/inspector --url http://localhost:8080/mcp

# ── Build ──────────────────────────────────────────────────────────────────────
.PHONY: build
build: ## Build static binary to ./bin/
	mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) $(CMD)

.PHONY: docker-build
docker-build: ## Build Docker image
	docker build -t $(IMAGE):$(TAG) .

.PHONY: docker-push
docker-push: docker-build ## Push image to ACR
	docker tag $(IMAGE):$(TAG) $(REGISTRY)/$(IMAGE):$(TAG)
	docker push $(REGISTRY)/$(IMAGE):$(TAG)

# ── Quality ────────────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run tests
	go test ./...

.PHONY: lint
lint: ## Run golangci-lint (brew install golangci-lint)
	golangci-lint run ./...

.PHONY: tidy
tidy: ## Tidy and verify modules
	go mod tidy
	go mod verify

# ── Help ───────────────────────────────────────────────────────────────────────
.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
