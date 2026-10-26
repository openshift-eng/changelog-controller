# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	lib/tmp.mk \
)

# Build configuration
git_commit=$(shell git describe --tags --always --dirty)
build_date=$(shell date -u '+%Y%m%d')
version=v${build_date}-${git_commit}

SOURCE_GIT_TAG=v1.0.0+$(shell git rev-parse --short=7 HEAD)

GOLINT=golangci-lint

GO_LD_EXTRAFLAGS=-X github.com/openshift-eng/changelog-controller/vendor/k8s.io/client-go/pkg/version.gitCommit=$(shell git rev-parse HEAD) -X github.com/openshift-eng/changelog-controller/vendor/k8s.io/client-go/pkg/version.gitVersion=${SOURCE_GIT_TAG} -X k8s.io/test-infra/prow/version.Name=changelog-controller -X k8s.io/test-infra/prow/version.Version=${version}
GO_TEST_FLAGS=

sonar-reports:
	go test ./... -coverprofile=coverage.out -covermode=count -json > report.json
	golangci-lint run ./... --verbose --no-config --out-format checkstyle --issues-exit-code 0 > golangci-lint.out
.PHONY: sonar-reports

lint:
	@echo "Running linter..."
	@if command -v $(GOLINT) >/dev/null 2>&1; then \
		$(GOLINT) run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi
.PHONY: lint

verify: lint
.PHONY: verify

vendor:
	go mod tidy
	go mod vendor
.PHONY: vendor

# Override the vulncheck target from build-machinery-go to use our wrapper
# that supports ignoring vulnerabilities with no available fix
vulncheck:
	./hack/govulncheck-wrapper.sh
.PHONY: vulncheck
