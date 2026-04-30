# Convenience targets for fair. The actual CLI is `fair`; targets
# below are sugar for the common flows.
#
# Profile is required by every fair command. Default is sandbox; override
# with `make publish-post POST=path/to/index.md PROFILE=prod`.

PROFILE ?= sandbox
CA_BUNDLE := $(shell mkcert -CAROOT 2>/dev/null)/rootCA.pem
CA_FLAG := $(if $(wildcard $(CA_BUNDLE)),--ca-bundle $(CA_BUNDLE),)

.PHONY: help
help:
	@echo "Usage: make <target> [PROFILE=<name>] [POST=<path>]"
	@echo
	@echo "Targets:"
	@echo "  build           Compile ./fair"
	@echo "  test            Run go test ./..."
	@echo "  vet             Run go vet ./..."
	@echo "  install         Build and install to \$$GOBIN"
	@echo
	@echo "  sandbox-up      Bootstrap the local PDS + create test account"
	@echo "  sandbox-down    Stop the sandbox containers (keeps volume)"
	@echo "  sandbox-reset   Wipe sandbox data (volume + state + sessions)"
	@echo
	@echo "  ls              fair ls"
	@echo "  doctor          fair doctor"
	@echo "  publish-post    fair publish POST=<path>"
	@echo "  build-site      fair build"
	@echo "  preview         Run a static server over dist.\$$(PROFILE)/"
	@echo
	@echo "Current PROFILE=$(PROFILE)"

.PHONY: build
build: fair

fair: $(shell find cmd internal -name '*.go' 2>/dev/null) go.mod go.sum
	go build -o fair ./cmd/fair

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: install
install:
	go install ./cmd/fair

# --- Sandbox lifecycle -------------------------------------------------------

.PHONY: sandbox-up
sandbox-up:
	bash sandbox/seed.sh

.PHONY: sandbox-down
sandbox-down:
	docker compose -f sandbox/compose.yaml --env-file sandbox/.env down

.PHONY: sandbox-reset
sandbox-reset:
	bash sandbox/reset.sh

# --- Blog operations --------------------------------------------------------

.PHONY: ls
ls: fair
	./fair --profile $(PROFILE) ls $(CA_FLAG)

.PHONY: doctor
doctor: fair
	./fair --profile $(PROFILE) doctor $(CA_FLAG)

.PHONY: publish-post
publish-post: fair
	@if [ -z "$(POST)" ]; then \
		echo "Usage: make publish-post POST=path/to/index.md"; exit 1; \
	fi
	./fair --profile $(PROFILE) publish $(POST) $(CA_FLAG)

.PHONY: build-site
build-site: fair
	./fair --profile $(PROFILE) build $(CA_FLAG)

.PHONY: preview
preview: build-site
	@echo "Serving dist.$(PROFILE)/ at http://localhost:8000/"
	python3 -m http.server 8000 --directory dist.$(PROFILE)

# --- Tangled push (after the remote is configured) -------------------------

.PHONY: push-tangled
push-tangled:
	git push tangled --all
	git push tangled --tags

# --- Cleanup ---------------------------------------------------------------

.PHONY: clean
clean:
	rm -f fair
	rm -rf dist dist.tmp dist.sandbox dist.sandbox.tmp
	rm -rf images-mirror images-mirror.sandbox images-mirror.prod
