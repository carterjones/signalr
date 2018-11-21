build:
	go build -mod=vendor

test:
	go test -race ./...

cover:
	go test -coverprofile=c.out -covermode=atomic -race ./...
	cp c.out coverage.txt

lint:
	/bin/bash -c "GO111MODULE=off go get -u github.com/alecthomas/gometalinter"
	gometalinter --install
	$(GOPATH)/bin/gometalinter \
	--cyclo-over 12 \
	--disable gotype \
	--disable gotypex \
	--enable nakedret \
	--exclude "/usr/local/go/src/" \
	--vendor \
	./...

update:
	go get -u ./...
	go mod tidy
	go mod vendor
	pre-commit autoupdate
