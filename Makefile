.PHONY: run test fmt vet check build docker

run:
	go run ./cmd/rdpweb

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go')

vet:
	go vet ./...

check:
	test -z "$$(gofmt -l $$(find . -name '*.go'))"
	go test ./...
	go vet ./...
	go build ./cmd/rdpweb

build:
	CGO_ENABLED=0 go build -trimpath -o rdpweb ./cmd/rdpweb

docker:
	docker build -t rdp-web:local .
