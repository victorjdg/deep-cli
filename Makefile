VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION)"
BINARY := deepseek
GOFLAGS := -trimpath

.PHONY: build install clean build-all

build:
	go build $(GOFLAGS) $(LDFLAGS) -o $(BINARY) .

install: build
	cp $(BINARY) $(GOPATH)/bin/$(BINARY) 2>/dev/null || sudo cp $(BINARY) /usr/local/bin/$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf dist/

build-all: clean
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) $(LDFLAGS) -o dist/$(BINARY)-linux-arm64 .
