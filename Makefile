GOCMD=go
GOBUILD=$(GOCMD) build
BINARY_NAME=./bin/adapter

all: test build run_server

wire:
	wire gen ./cmd/splitty
	echo "wire build"

build:
	$(GOBUILD) -o $(BINARY_NAME) ./cmd/splitty
	echo "binary build"

run_server:
	 LOG_LEVEL=debug $(BINARY_NAME)

