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
	go tool controller-gen rbac:roleName=manager-role,headerFile="./contrib/yaml-license-header.txt" crd:headerFile="./contrib/yaml-license-header.txt" webhook:headerFile="./contrib/yaml-license-header.txt" paths="./pkg/..." output:crd:artifacts:config=contrib/k8s/crd output:rbac:artifacts:config=contrib/k8s/rbac

.PHONY: helm
helm:
	cp contrib/k8s/crd/ceph.cobaltcore.sap.com_remotearbiters.yaml contrib/charts/external-arbiter-operator/templates/remotearbiter-crd.yaml
	cp contrib/k8s/crd/ceph.cobaltcore.sap.com_remoteclusters.yaml contrib/charts/external-arbiter-operator/templates/remotecluster-crd.yaml

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

.PHONY: env
env:
	go tool setup-envtest use $(K8S_VERSION) --bin-dir ./.env -p path

.PHONY: deps
deps:
	-git clone https://github.com/rook/rook.git
	cd rook && git checkout v$(ROOK_VERSION)
	mkdir -p contrib/k8s/3rdparty
	cp -r rook/deploy/examples/crds.yaml contrib/k8s/3rdparty/rook.yaml

.PHONY: test
test: pretty env deps
	go test ./...

.PHONY: clean
clean:
	rm -rf build/
