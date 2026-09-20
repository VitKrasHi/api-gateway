.PHONY: test test-race test-cover test-verbose lint

test:
	go test ./...

test-race:
	go test -race ./...

test-cover:
	go test -race -coverprofile=cover.out -covermode=atomic ./...
	go tool cover -html=cover.out -o cover.html
	@echo "Отчёт: cover.html"

test-verbose:
	go test -v -race ./...

bench:
	go test -bench=. -benchmem ./internal/middleware/

lint:
	go vet ./...
	golangci-lint run