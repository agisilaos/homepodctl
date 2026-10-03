.PHONY: build test vet fmt fmt-check check-help update-help docs-check verify verify-core release-fixtures changelog-context release-check release-check-ci release release-dry-run

build:
	go build -o homepodctl ./cmd/homepodctl

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd/homepodctl/*.go internal/music/*.go internal/native/*.go

fmt-check:
	@files="$$(gofmt -l cmd internal)" || exit 1; \
	if [ -n "$$files" ]; then printf 'Run make fmt to format:\n%s\n' "$$files"; exit 1; fi

check-help:
	./scripts/check-help.sh

update-help:
	./scripts/update-help.sh

docs-check:
	./scripts/docs-check.sh

verify:
	./scripts/release-check.sh
	$(MAKE) release-fixtures

verify-core:
	./scripts/verify.sh

release-fixtures:
	./scripts/release-check-test.sh
	python3 ./scripts/release-publication-test.py
	python3 ./scripts/release-publisher-contract-test.py

changelog-context:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make changelog-context VERSION=v0.1.0)"; exit 2; fi
	./scripts/changelog-context.sh "$(VERSION)"

release-check:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release-check VERSION=v0.1.0)"; exit 2; fi
	./scripts/release-check.sh "$(VERSION)"

release-check-ci:
	./scripts/release-check.sh --ci

release:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release VERSION=v0.1.0)"; exit 2; fi
	./scripts/release.sh "$(VERSION)"

release-dry-run:
	@if [ -z "$(VERSION)" ]; then echo "VERSION is required (e.g. make release-dry-run VERSION=v0.1.0)"; exit 2; fi
	./scripts/release.sh "$(VERSION)" --dry-run
