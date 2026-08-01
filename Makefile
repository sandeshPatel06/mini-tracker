WAILS_TAGS = -tags webkit2_41

ifneq ("$(wildcard $(HOME)/.local/wails_env.sh)","")
  export PATH := $(HOME)/.local/bin:$(HOME)/.local/usr/bin:$(PATH)
  export PKG_CONFIG_PATH := $(HOME)/.local/usr/lib/x86_64-linux-gnu/pkgconfig:$(HOME)/.local/usr/lib/pkgconfig:$(HOME)/.local/usr/share/pkgconfig:$(PKG_CONFIG_PATH)
  export LIBRARY_PATH := $(HOME)/.local/usr/lib/x86_64-linux-gnu:$(HOME)/.local/usr/lib:$(LIBRARY_PATH)
  export LD_LIBRARY_PATH := $(HOME)/.local/usr/lib/x86_64-linux-gnu:$(HOME)/.local/usr/lib:$(LD_LIBRARY_PATH)
  export C_INCLUDE_PATH := $(HOME)/.local/usr/include:$(HOME)/.local/usr/include/x86_64-linux-gnu:$(C_INCLUDE_PATH)
  export CPLUS_INCLUDE_PATH := $(HOME)/.local/usr/include:$(HOME)/.local/usr/include/x86_64-linux-gnu:$(CPLUS_INCLUDE_PATH)
  export CGO_CFLAGS := -I$(HOME)/.local/usr/include -I$(HOME)/.local/usr/include/x86_64-linux-gnu $(CGO_CFLAGS)
  export CGO_LDFLAGS := -L$(HOME)/.local/usr/lib -L$(HOME)/.local/usr/lib/x86_64-linux-gnu $(CGO_LDFLAGS)
endif

HAS_PKG_CONFIG := $(shell which pkg-config 2>/dev/null)

.DEFAULT_GOAL := help

.PHONY: help default all dev build run-server build-server clean doctor setup-input setup-debs docker-up docker-down docker-logs vet tidy clean-bin install

default: help
all: help

help: ## Show this help message
	@echo "Mini Tracker Makefile commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
	@echo ""

install: ## Run automated Linux installer (builds app & configures systemd daemon)
	./install.sh

setup-debs: ## Install GTK3/WebKit2GTK dependencies without sudo into ~/.local
	./scripts/install-debs-user.sh

run-server: build-server ## Run full web app & tracker without sudo/GTK/pkg-config
	./server

build-server: ## Build zero-dependency web app binary
	npm --prefix frontend run build
	go build -o server ./cmd/server

build: ## Build production desktop binary (or server fallback)
ifdef HAS_PKG_CONFIG
	wails build $(WAILS_TAGS) -clean
else
	@echo "⚠️  pkg-config not found — building zero-dependency web server binary './server' instead..."
	$(MAKE) build-server
endif

dev: ## Run in dev mode (falls back to web dev server if pkg-config missing)
ifdef HAS_PKG_CONFIG
	wails dev $(WAILS_TAGS)
else
	@echo "⚠️  pkg-config is not installed on this machine."
	@echo "To install dependencies without sudo, run:"
	@echo "  make setup-debs"
	@echo ""
	@echo "Starting web-based dev server instead..."
	$(MAKE) run-server
endif

doctor: ## Check Wails environment dependencies
	wails doctor

setup-input: ## Add user to 'input' group for keystroke tracking (requires logout after)
	sudo usermod -aG input $$USER
	@echo "✓ Added to input group. Please log out and back in."

docker-up: ## Run backend-only API server using Docker
	docker compose up --build -d

docker-down: ## Stop Docker containers
	docker compose down

docker-logs: ## View Docker container logs
	docker compose logs -f

vet: ## Run go vet check
	go vet ./...

tidy: ## Run go mod tidy
	go mod tidy

clean-bin: ## Remove built binary files
	rm -rf build/bin server

