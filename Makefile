GO ?= go
GO_TEST_ARGS ?=
COVERAGE_PROFILE ?=
COVERAGE_MODE ?= atomic

COVERAGE_ARGS := $(if $(COVERAGE_PROFILE),-coverprofile="$(COVERAGE_PROFILE)" -covermode="$(COVERAGE_MODE)",)

.PHONY: build test-unit test-e2e-compile fmt-check setup-minikube-same-network delete-minikube-same-network

build:
	"$(GO)" build ./...
	"$(GO)" build -o crane .

test-unit:
	@set -eu; \
	packages="$$(GO="$(GO)" bash ./hack/unit-packages.sh)"; \
	test -n "$$packages"; \
	"$(GO)" test $(GO_TEST_ARGS) $(COVERAGE_ARGS) $$packages

test-e2e-compile:
	"$(GO)" test $(GO_TEST_ARGS) -run '^$$' -bench '^$$' ./e2e-tests/tests/...

fmt-check:
	bash ./hack/fmt-check.sh

setup-minikube-same-network:
	bash ./hack/setup-minikube-same-network.sh

delete-minikube-same-network:
	minikube delete -p src || true
	minikube delete -p tgt || true
