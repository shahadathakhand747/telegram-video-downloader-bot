# Makefile for Telegram Video Downloader Bot

.PHONY: all build run test clean docker-build docker-run lint fmt vet help

# Binary name
BINARY_NAME=telegram-video-bot
BINARY_NAME_WIN=$(BINARY_NAME).exe

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Docker parameters
DOCKER_IMAGE=telegram-video-bot
DOCKER_TAG=latest

# Build flags
LDFLAGS=-ldflags "-s -w"

all: clean build

## build: Build the binary for Linux
build:
	@echo "Building $(BINARY_NAME)..."
	@CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) .

## build-windows: Build the binary for Windows
build-windows:
	@echo "Building $(BINARY_NAME_WIN) for Windows..."
	@GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME_WIN) .

## build-arm: Build the binary for ARM
build-arm:
	@echo "Building $(BINARY_NAME) for ARM..."
	@GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-arm .

## run: Build and run the bot locally
run: build
	@echo "Running $(BINARY_NAME)..."
	@./$(BINARY_NAME)

## test: Run all tests
test:
	@echo "Running tests..."
	@$(GOTEST) -v ./...

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@$(GOCLEAN)
	@rm -f $(BINARY_NAME) $(BINARY_NAME_WIN) $(BINARY_NAME)-arm
	@rm -rf dist/

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	@$(GOMOD) download
	@$(GOMOD) tidy

## lint: Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run ./...

## fmt: Format code
fmt:
	@echo "Formatting code..."
	@$(GOFMT) -s -w .

## vet: Run go vet
vet:
	@echo "Running go vet..."
	@$(GOVET) ./...

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

## docker-run: Run bot in Docker
docker-run:
	@echo "Running bot in Docker..."
	@docker run -d --name $(BINARY_NAME) \
		-e TELEGRAM_BOT_TOKEN=$$TELEGRAM_BOT_TOKEN \
		-e RENDER_EXTERNAL_URL=$$RENDER_EXTERNAL_URL \
		-p 8080:8080 \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

## docker-stop: Stop Docker container
docker-stop:
	@echo "Stopping Docker container..."
	@docker stop $(BINARY_NAME) || true
	@docker rm $(BINARY_NAME) || true

## docker-clean: Remove Docker image
docker-clean:
	@echo "Removing Docker image..."
	@docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) || true

## install-yt-dlp: Install yt-dlp
install-yt-dlp:
	@echo "Installing yt-dlp..."
	@pip install yt-dlp

## update-yt-dlp: Update yt-dlp
update-yt-dlp:
	@echo "Updating yt-dlp..."
	@yt-dlp -U

## help: Show this help
help:
	@echo "Telegram Video Downloader Bot - Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build          Build the binary for Linux"
	@echo "  make run           Build and run locally"
	@echo "  make test          Run tests"
	@echo "  make clean         Clean build artifacts"
	@echo "  make deps          Download dependencies"
	@echo "  make docker-build  Build Docker image"
	@echo "  make docker-run    Run bot in Docker"
	@echo "  make help          Show this help"
