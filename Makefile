BINARY_NAME=zkteco-api
BIN_DIR=bin
LDFLAGS=-ldflags="-s -w"

.PHONY: all build build-all build-windows build-linux build-mac run clean

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./cmd/api
	@echo "Built $(BIN_DIR)/$(BINARY_NAME)"

build-windows:
	@mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME).exe ./cmd/api
	@echo "Built Windows binary: $(BIN_DIR)/$(BINARY_NAME).exe"

build-linux:
	@mkdir -p $(BIN_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/api
	@echo "Built Linux binary: $(BIN_DIR)/$(BINARY_NAME)-linux-amd64"

build-mac:
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/api
	@echo "Built macOS (Apple Silicon) binary: $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64"

build-all: build-windows build-linux build-mac

run:
	go run ./cmd/api

clean:
	rm -rf $(BIN_DIR)
