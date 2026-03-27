.PHONY: all format test build lint

all: format test build lint

format:
	go fmt ./...

test:
	go test -v ./...

build:
	go build -o ./bin/targetgroup-senpai .

lint:
	golangci-lint run
