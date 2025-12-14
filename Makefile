.PHONY: build test vet fmt release

build:
	go build -o homepodctl ./cmd/homepodctl

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd/homepodctl/main.go internal/music/music.go internal/native/native.go internal/music/music_test.go

release:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release VERSION=v0.1.0)"; exit 2; fi
	./scripts/release.sh "$(VERSION)"
