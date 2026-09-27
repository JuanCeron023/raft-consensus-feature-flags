# FlagForge — build, test, and dev tasks.
# Run `make help` for the full list.

BINARY := flagforge
PKG    := ./...

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## --- Backend (Go) ---------------------------------------------------------

.PHONY: build
build: ## Compile the Go binary
	go build -o $(BINARY) ./cmd/$(BINARY)

.PHONY: run
run: ## Run a single local node
	go run ./cmd/$(BINARY) --id A --http :9001 --raft :7001 --peers "A=localhost:7001"

.PHONY: test
test: ## Run tests
	go test $(PKG) -count=1

.PHONY: race
race: ## Run tests with the race detector
	go test $(PKG) -race -count=1

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: fmt
fmt: ## Format all Go files
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any Go file is not gofmt-formatted
	@f=$$(gofmt -l .); if [ -n "$$f" ]; then echo "Not formatted:"; echo "$$f"; exit 1; fi

.PHONY: lint
lint: vet fmt-check ## Static checks (vet + format check)

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

.PHONY: check
check: lint race ## Everything CI runs: lint + race tests

## --- Frontend (React + TypeScript) ---------------------------------------

.PHONY: frontend-install
frontend-install: ## Install frontend dependencies
	cd frontend && npm install

.PHONY: frontend-build
frontend-build: ## Build the React frontend into web/static
	cd frontend && npm run build

.PHONY: frontend-dev
frontend-dev: ## Run the Vite dev server (proxies API/WS to the backend)
	cd frontend && npm run dev

## --- Docker ---------------------------------------------------------------

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -t $(BINARY) .

.PHONY: up
up: ## Start the full stack with docker compose
	docker compose up --build

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf web/static frontend/node_modules
