SHELL := /bin/sh

GO_IMAGE := golang:1.23-alpine
GO_MOD_CACHE := parkakil-gomod
GO_DOCKER := docker run --rm -v "$(CURDIR):/src" -v $(GO_MOD_CACHE):/go/pkg/mod -w /src -e GOFLAGS=-buildvcs=false $(GO_IMAGE) sh -c

.PHONY: up down logs test smoke

up:
	docker compose up -d --build --wait

down:
	docker compose down -v --remove-orphans

logs:
	docker compose logs -f --tail=100

test:
	$(GO_DOCKER) "go vet ./... && go test ./..."

smoke:
	node test/faz2.js
