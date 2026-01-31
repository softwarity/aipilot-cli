VERSION := $(shell cat VERSION)

# Default relay URL (can be overridden by .env.local or environment)
RELAY_URL ?= wss://aipilot-relay.softwarity.io

# Load .env.local if it exists (for local dev)
-include .env.local

LDFLAGS := -ldflags="-s -w -X main.Version=v$(VERSION) -X main.RelayURL=$(RELAY_URL)"

.PHONY: build run version patch minor major release clean

# Build binary
build:
	go build $(LDFLAGS) -o aipilot-cli .

# Run locally
run:
	go run $(LDFLAGS) .

# Clean build artifacts
clean:
	rm -f aipilot-cli aipilot-cli-*

# Show current version
version:
	@echo "v$(VERSION)"

# Bump patch version (0.0.X)
patch:
	@V=$$(cat VERSION); \
	MAJOR=$$(echo $$V | cut -d. -f1); \
	MINOR=$$(echo $$V | cut -d. -f2); \
	PATCH=$$(echo $$V | cut -d. -f3); \
	NEW_PATCH=$$((PATCH + 1)); \
	echo "$$MAJOR.$$MINOR.$$NEW_PATCH" > VERSION; \
	echo "Bumped to v$$MAJOR.$$MINOR.$$NEW_PATCH"

# Bump minor version (0.X.0)
minor:
	@V=$$(cat VERSION); \
	MAJOR=$$(echo $$V | cut -d. -f1); \
	MINOR=$$(echo $$V | cut -d. -f2); \
	NEW_MINOR=$$((MINOR + 1)); \
	echo "$$MAJOR.$$NEW_MINOR.0" > VERSION; \
	echo "Bumped to v$$MAJOR.$$NEW_MINOR.0"

# Bump major version (X.0.0)
major:
	@V=$$(cat VERSION); \
	MAJOR=$$(echo $$V | cut -d. -f1); \
	NEW_MAJOR=$$((MAJOR + 1)); \
	echo "$$NEW_MAJOR.0.0" > VERSION; \
	echo "Bumped to v$$NEW_MAJOR.0.0"

# Release: bump, commit, tag, push
release-patch: patch release
release-minor: minor release
release-major: major release

release:
	@V=$$(cat VERSION); \
	git add VERSION; \
	git commit -m "Bump version to v$$V"; \
	git tag "v$$V"; \
	git push && git push --tags; \
	echo "Released v$$V"
