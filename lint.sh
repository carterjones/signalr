#!/bin/bash

go get -u github.com/alecthomas/gometalinter
$GOPATH/bin/gometalinter --install &> /dev/null
$GOPATH/bin/gometalinter \
    --cyclo-over 12 \
    --disable gotype \
    ./...
