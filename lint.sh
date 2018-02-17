#!/bin/bash

go get -u github.com/alecthomas/gometalinter
$GOPATH/bin/gometalinter --install &> /dev/null

set -euxo pipefail

$GOPATH/bin/gometalinter \
    --cyclo-over 12 \
    --disable gotype \
    --disable gotypex \
    --vendor \
    ./...
