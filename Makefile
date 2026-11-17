.PHONY: build test clean

build:
	go build -v -o bin/ -ldflags "-s -w -X main.version=$$(git describe --tags --always)" ./...

test:
	go vet ./...
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

clean:
	rm -rf bin/ coverage.out
