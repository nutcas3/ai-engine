TEMPL_VERSION ?= v0.3.1100
TAILWIND_VERSION ?= v4.3.4

all: verify

templ-install:
	@if ! command -v templ > /dev/null; then \
		go install github.com/a-h/templ/cmd/templ@$(TEMPL_VERSION); \
	fi

tailwind-install:
	@if [ ! -x ./tailwindcss ]; then \
		os=$$(uname -s); arch=$$(uname -m); \
		case "$$os/$$arch" in \
			Darwin/arm64) asset=macos-arm64 ;; \
			Darwin/x86_64) asset=macos-x64 ;; \
			Linux/aarch64|Linux/arm64) asset=linux-arm64 ;; \
			Linux/x86_64) asset=linux-x64 ;; \
			*) echo "Unsupported Tailwind platform: $$os/$$arch" >&2; exit 1 ;; \
		esac; \
		curl -fsSL "https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-$$asset" -o tailwindcss; \
		chmod +x tailwindcss; \
	fi

generate: templ-install
	@templ generate

css: tailwind-install
	@./tailwindcss -i cmd/web/styles/input.css -o cmd/web/assets/css/output.css

assets: generate css

build: assets
	@go build -o main ./cmd/api

verify: assets
	@go test ./...
	@go vet ./...
	@go build ./...

run: assets
	@go run cmd/api/main.go

# Create DB container
docker-run:
	@if docker compose up --build 2>/dev/null; then \
		: ; \
	else \
		echo "Falling back to Docker Compose V1"; \
		docker-compose up --build; \
	fi

# Shutdown DB container
docker-down:
	@if docker compose down 2>/dev/null; then \
		: ; \
	else \
		echo "Falling back to Docker Compose V1"; \
		docker-compose down; \
	fi

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v

# Clean the binary
clean:
	@echo "Cleaning..."
	@rm -f main

# Live Reload
watch:
	@if command -v air > /dev/null; then \
            air; \
            echo "Watching...";\
        else \
            read -p "Go's 'air' is not installed on your machine. Do you want to install it? [Y/n] " choice; \
            if [ "$$choice" != "n" ] && [ "$$choice" != "N" ]; then \
                go install github.com/air-verse/air@latest; \
                air; \
                echo "Watching...";\
            else \
                echo "You chose not to install air. Exiting..."; \
                exit 1; \
            fi; \
        fi

.PHONY: all assets build clean css generate run test verify watch tailwind-install templ-install
