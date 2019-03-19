build:
	go build -mod=vendor

test:
	go test -race ./...

cover:
	go test -coverprofile=c.out -covermode=atomic -race ./...
	cp c.out coverage.txt

lint:
	/bin/bash -c "golangci-lint --version | grep -q 1.15.0 || curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b $(GOPATH)/bin v1.15.0"
	golangci-lint run --enable-all

update:
	go get -u ./...
	go mod tidy
	go mod vendor -v
	pre-commit autoupdate
