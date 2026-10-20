.PHONY: lint test build docker snapshot

# Overridable from the outside: make build VERSION=v0.1.0. git describe reports
# the tag when HEAD sits on one, and the commit with a -dirty suffix otherwise,
# so a local build is still identifiable.
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT  ?= $(shell git rev-parse --short HEAD)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)

lint:
	golangci-lint run ./...

test:
	go test ./... -count=1

# CGO off keeps the binary static; -trimpath drops the build machine's paths
# out of it.
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/tamper ./cmd/tamper

# The image gets the same two values, which it cannot work out on its own: the
# build context carries no git repository.
docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t tamper:$(VERSION) .

# A dry run of the release: builds the whole matrix into dist/ and publishes
# nothing, so the configuration can be checked without pushing a tag.
snapshot:
	goreleaser release --snapshot --clean
