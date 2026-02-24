.PHONY: build generate crds fmt vet tidy clean docker-build

build: generate fmt vet
	CGO_ENABLED=0 go build -o bin/provider ./cmd/provider/

generate:
	go generate ./...
	go tool controller-gen object paths=./apis/...

crds:
	go tool controller-gen crd paths=./apis/... output:crd:dir=package/crds

fmt:
	goimports -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/ package/crds/*.yaml

docker-build:
	docker build -t provider-valkey .

all: tidy generate crds build
