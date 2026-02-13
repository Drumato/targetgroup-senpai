.PHONY: all format test build lint

all: format test build lint

format:
	go fmt ./...

test:
	go test -v ./...

build:
	go build -o ./bin/targetgroup-senpai .

lint:
	@which golangci-lint > /dev/null || (curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b tools v2.9.0)
	./tools/golangci-lint run
