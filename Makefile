ROLE := observer-gateway
.PHONY: build test vet
build:
	go build -o bin/cofiswarm-observer-gateway ./cmd/cofiswarm-observer-gateway
vet:
	go vet ./...
test: vet
	go test ./...
