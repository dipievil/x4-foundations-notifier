BINARY := dist/x4-notifier
GO     ?= go

.PHONY: help build release test vet fmt mod-package clean

help:
	@echo "build       compile the Go server into $(BINARY)"
	@echo "release     build the shipped Windows and Linux binaries into bin/"
	@echo "test        run the Go tests"
	@echo "vet         run go vet"
	@echo "fmt         run gofmt -l (prints files that need formatting)"
	@echo "mod-package build dist/fleet_notifier.zip for the game's extensions folder"
	@echo "clean       remove dist/"

build:
	mkdir -p dist
	cd go_server && $(GO) build -trimpath -o ../$(BINARY) .

release:
	GO=$(GO) ./scripts/build-release.sh

test:
	cd go_server && $(GO) test -count=1 ./...

vet:
	cd go_server && $(GO) vet ./...

fmt:
	cd go_server && gofmt -l .

mod-package:
	./scripts/package-mod.sh

clean:
	rm -rf dist
