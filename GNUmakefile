default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	go vet ./...

# The docs under docs/, from the schemas and examples/.
generate:
	go generate ./...

fmt:
	gofmt -s -w -e .
	terraform fmt -recursive examples

# Unit tests: Terraform itself against a yet kept in memory.
test:
	go test -v -cover -timeout=120s -parallel=10 ./...

# Against a real company: YET_API_KEY (and YET_CLOUD_URL, YET_WORKSPACE_URL
# for a yet on a laptop). They make things there, and remove them after.
testacc:
	TF_ACC=1 go test -v -cover -timeout 120m ./...

.PHONY: fmt lint test testacc build install generate
