build:
	go build

test:
	go test -race ./...

cover:
	go test -coverprofile=c.out -covermode=atomic -race ./...

lint:
	$(GOPATH)/bin/gometalinter \
	--cyclo-over 12 \
	--disable gotype \
	--disable gotypex \
	--enable nakedret \
	--exclude "/usr/local/go/src/" \
	--vendor \
	./...
