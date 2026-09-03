GIT_COMMIT=$(shell git log -1 --format=%H)
GIT_TAG=$(shell git symbolic-ref -q --short HEAD || git describe --tags --exact-match)
BUILD_DATE=$(shell date -Is -u)

K8S_VERSION="1.34.1"
ROOK_VERSION="1.18.6"

.PHONY: all
all: operator

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: gen
gen:
	go tool controller-gen object:headerFile="./contrib/go-license-header.txt" paths="./pkg/..."
	go tool controller-gen rbac:roleName=manager-role,headerFile="./contrib/yaml-license-header.txt" crd:headerFile="./contrib/yaml-license-header.txt" webhook:headerFile="./contrib/yaml-license-header.txt" paths="./pkg/..." output:crd:artifacts:config="./contrib/k8s/crd" output:rbac:artifacts:config="./contrib/k8s/rbac" output:webhook:artifacts:config="./contrib/k8s/webhook"

.PHONY: helm
helm: gen
	cp contrib/k8s/crd/ceph.cobaltcore.sap.com_remotearbiters.yaml contrib/charts/external-arbiter-operator/templates/remotearbiter-crd.yaml
	cp contrib/k8s/crd/ceph.cobaltcore.sap.com_remoteclusters.yaml contrib/charts/external-arbiter-operator/templates/remotecluster-crd.yaml
	go tool yq '.rules' ./contrib/k8s/rbac/role.yaml -o y | { tmp=$$(mktemp) && (sed '/^rules:/q' ./contrib/charts/external-arbiter-operator/templates/manager-role.yaml; cat) > "$${tmp}" && mv "$${tmp}" ./contrib/charts/external-arbiter-operator/templates/manager-role.yaml; }

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: imports
imports:
	go tool goimports -local github.com/cobaltcore-dev/external-arbiter-operator -w ./cmd
	go tool goimports -local github.com/cobaltcore-dev/external-arbiter-operator -w ./pkg

.PHONY: fieldalignment
fieldalignment:
	until go tool betteralign -apply ./pkg/... ./cmd/... ; do :; done

.PHONY: lint
lint:
	go tool golangci-lint run

.PHONY: vuln
vuln:
	go tool govulncheck ./...

.PHONY: license
license:
	find . -name "*.go" | xargs go tool addlicense -c="SAP SE or an SAP affiliate company and cobaltcore-dev contributors" -l="apache" -s="only"

.PHONY: pretty
pretty: tidy gen fmt vet imports fieldalignment lint vuln license

.PHONY: mkdir-build
mkdir-build: 
	mkdir -p build

%-bin: pretty mkdir-build
	:

.PHONY: operator
operator: manager-bin
	go build -ldflags="-X 'main.date=$(BUILD_DATE)' -X 'main.version=$(GIT_TAG)' -X 'main.commit=$(GIT_COMMIT)'" -o build/manager cmd/manager/main.go

.PHONY: env
env:
	go tool setup-envtest use $(K8S_VERSION) --bin-dir ./.env -p path

.PHONY: deps
deps:
	-git clone https://github.com/rook/rook.git
	cd rook && git checkout v$(ROOK_VERSION)
	mkdir -p contrib/k8s/3rdparty
	cp -r rook/deploy/examples/crds.yaml contrib/k8s/3rdparty/rook.yaml

.PHONY: test-unit
test-unit:
	go test -race -shuffle=on -count=1 ./pkg/controller/... ./pkg/webhook/... ./test/...

# test-unit-repeat re-runs the unit suites REPEAT (default 20) times, each with a
# fresh shuffle/Ginkgo seed, to expose order dependence and flakes (#67). Ginkgo
# rejects `go test -count>1`, so the suite is looped instead.
REPEAT ?= 20
.PHONY: test-unit-repeat
test-unit-repeat:
	@for i in $$(seq 1 $(REPEAT)); do \
		echo "=== unit run $$i/$(REPEAT) ==="; \
		go test -race -shuffle=on -count=1 ./pkg/controller/... ./pkg/webhook/... ./test/... || exit 1; \
	done

.PHONY: test-envtest
test-envtest: env
	KUBEBUILDER_ASSETS="$(abspath $(shell go tool setup-envtest use $(K8S_VERSION) --bin-dir ./.env -p path))" \
		go test -tags envtest -shuffle=on -count=1 ./pkg/controller/...

.PHONY: test-all
test-all: test-unit test-envtest

# test-cover reports statement coverage for the reconciler and webhook packages,
# merging the pure-unit and envtest (build-tagged) runs into one profile. The
# controller package is exercised almost entirely by the envtest suite, so a
# unit-only profile understates it — both runs must be merged. This target
# REPORTS a number; it does not gate. Set a floor with COVER_MIN once the number
# is stable, then ratchet it up as gaps in docs/testing/traceability.md close.
# ponytail: report-only until a measured floor exists; gate via COVER_MIN when set.
COVER_MIN ?=
.PHONY: test-cover
test-cover: env
	go test -race -shuffle=on -count=1 -coverprofile=cover.unit.out \
		-coverpkg=./pkg/... ./pkg/webhook/... ./test/...
	KUBEBUILDER_ASSETS="$(abspath $(shell go tool setup-envtest use $(K8S_VERSION) --bin-dir ./.env -p path))" \
		go test -tags envtest -shuffle=on -count=1 -coverprofile=cover.envtest.out \
		-coverpkg=./pkg/... ./pkg/controller/...
	@{ echo "mode: atomic"; \
	   grep -h -v '^mode:' cover.unit.out cover.envtest.out; } > cover.out
	@go tool cover -func=cover.out | tail -1
	@total=$$(go tool cover -func=cover.out | tail -1 | grep -oE '[0-9]+\.[0-9]+'); \
	if [ -n "$(COVER_MIN)" ]; then \
		awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN{ if (t+0 < m+0){ printf "coverage %.1f%% below floor %s%%\n", t, m; exit 1 } else { printf "coverage %.1f%% meets floor %s%%\n", t, m } }'; \
	fi


# test runs the full layered suite (unit + envtest). It does not mutate source
# or run `pretty`. It does NOT clone Rook: the envtest suite loads CephCluster
# CRDs from contrib/k8s/3rdparty/, which `make deps` populates (a one-time
# network clone) — on a checkout without that directory, run `make deps` first.
# `make env` still downloads the envtest kube binaries on a cache miss.
.PHONY: test
test: test-all

.PHONY: clean
clean:
	rm -rf build/
