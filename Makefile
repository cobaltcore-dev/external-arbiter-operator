GIT_COMMIT=$(shell git log -1 --format=%H)
GIT_TAG=$(shell git symbolic-ref -q --short HEAD || git describe --tags --exact-match)
BUILD_DATE=$(shell date -Is -u)

K8S_VERSION="1.34.1"

.PHONY: all
all: operator

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: gen
gen:
	go tool controller-gen object:headerFile="./contrib/license-header.txt" paths="./..."
	go tool controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=contrib/k8s/crd output:rbac:artifacts:config=contrib/k8s/rbac

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

.PHONY: manager
operator: manager-bin
	go build -ldflags="-X 'main.date=$(BUILD_DATE)' -X 'main.version=$(GIT_TAG)' -X 'main.commit=$(GIT_COMMIT)'" -o build/manager cmd/manager/main.go

.PHONY: test
test: pretty env
	go test ./...

.PHONY: clean
clean:
	rm -rf build/

.PHONY: env
env:
	go tool setup-envtest use $(K8S_VERSION) --bin-dir ./.env -p path