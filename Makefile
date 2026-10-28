GO ?= go
VERSION ?= 0.1.0-dev

.PHONY: build test check release demo
build:
	$(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/xray-setup ./cmd/my-vpn

demo: build
	./dist/xray-setup --demo

test:
	$(GO) test ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...

release:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/xray-setup-linux-amd64 ./cmd/my-vpn
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/xray-setup-linux-arm64 ./cmd/my-vpn
