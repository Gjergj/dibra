SHELL=/bin/bash -eo pipefail

PWD = $(shell pwd)
GO ?= go

build:
	$(GO) build -o ./bin/dibra

test:
	$(GO) test -v ./...


install: build
	sudo cp bin/dibra /usr/local/bin/
	sudo chmod +x /usr/local/bin/dibra
