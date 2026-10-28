GO ?= go
VERSION ?= 0.1.0-dev

.PHONY: build test check release demo
build:
	$(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/my-vpn ./cmd/my-vpn

demo: build
	./dist/my-vpn --demo

test:
	$(GO) test ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/my-vpn-linux-amd64 ./cmd/my-vpn
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/my-vpn-linux-arm64 ./cmd/my-vpn
