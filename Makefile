build:
	go build

test:
	go test -race ./...

cover:
	go test -coverprofile=c.out -covermode=atomic -race ./...
	cp c.out coverage.txt

lint:
	go get -u github.com/alecthomas/gometalinter
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
	dep ensure -update
	pre-commit autoupdate
