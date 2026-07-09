.PHONY: build test cover clean qa-050 qa-051-sleepwake qa-051-permissions qa-052-installer

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

build:
	go build -ldflags="-s -w \
		-X 'main.version=$(VERSION)' \
		-X 'main.commit=$(COMMIT)' \
		-X 'main.date=$(DATE)'" -o cardbot .

test:
	go test ./... -count=1 -race

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out

qa-050:
	./scripts/qa_050_smoke.sh

qa-051-sleepwake:
	./scripts/qa_051_sleepwake_capture.sh

qa-051-permissions:
	./scripts/qa_051_permissions_capture.sh

qa-052-installer:
	./scripts/qa_052_installer_safety.sh

clean:
	rm -f cardbot coverage.out
