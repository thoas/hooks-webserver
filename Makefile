ROOT_DIR := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
BIN_DIR = $(ROOT_DIR)/bin

build-static:
	@(echo "-> Creating statically linked binary...")
	mkdir -p $(BIN_DIR)
	@(CGO_ENABLED=0 go build -a -installsuffix cgo -o $(BIN_DIR)/webserver)

docker-build:
	@(echo "-> Preparing builder...")
	@(docker build -t webserver-builder -f Dockerfile.build .)
	@(mkdir -p $(BIN_DIR))
	@(docker run --rm -v $(BIN_DIR):/go/src/github.com/thoas/webserver/bin webserver-builder)
	@(docker build -t webserver:latest .)

docker-run:
	@(docker run -p 8080:8080 -t webserver:latest)

test:
	go test ./...

lint:
	golangci-lint run .

live:
	air --build.cmd "make build-static" --build.bin "./bin/webserver"
