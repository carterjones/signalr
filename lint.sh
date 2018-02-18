#!/bin/bash
set -euxo pipefail

$GOPATH/bin/gometalinter \
    --cyclo-over 12 \
    --disable gotype \
    --disable gotypex \
    --exclude "/usr/local/go/src/" \
    --vendor \
    ./...
