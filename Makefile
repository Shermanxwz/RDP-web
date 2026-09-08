.PHONY: run test fmt vet check build docker container-smoke audit e2e

run:
	go run ./cmd/rdpweb

test:
	go test -race ./...

fmt:
	gofmt -w $$(find . -name '*.go')

vet:
	go vet ./...

check:
	test -z "$$(gofmt -l $$(find . -name '*.go'))"
	go test -race ./...
	go vet ./...
	CGO_ENABLED=0 go build -trimpath ./cmd/rdpweb

build:
	CGO_ENABLED=0 go build -trimpath -o rdpweb ./cmd/rdpweb

docker:
	docker build -t rdp-web:local .

container-smoke: docker
	RDPWEB_IMAGE=rdp-web:local bash scripts/container-smoke.sh

audit:
	npm ci
	npm audit --audit-level=high

e2e: audit
	npx playwright install chromium
	@echo "Start RDP Web on 127.0.0.1:18081 with RDPWEB_SETUP_TOKEN=browser-setup-token, then run:"
	@echo "npx playwright test tests/browser.spec.js --project=chromium"
