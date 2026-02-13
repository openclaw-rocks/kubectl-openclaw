BINARY_NAME := kubectl-openclaw
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X github.com/openclaw-rocks/kubectl-openclaw/cmd.Version=$(VERSION)

.PHONY: build clean test install uninstall lint

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) .

clean:
	rm -rf bin/ dist/

test:
	go test ./...

install: build
	cp bin/$(BINARY_NAME) $(shell go env GOPATH)/bin/

uninstall:
	rm -f $(shell go env GOPATH)/bin/$(BINARY_NAME)

lint:
	golangci-lint run ./...

# Cross-compile for release (without goreleaser)
release: clean
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-linux-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY_NAME)-darwin-arm64 .
	cd dist && for f in $(BINARY_NAME)-*; do tar czf $$f.tar.gz $$f; sha256sum $$f.tar.gz >> checksums.txt 2>/dev/null || shasum -a 256 $$f.tar.gz >> checksums.txt; done
