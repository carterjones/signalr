#!/bin/bash
set -euxo pipefail

$GOPATH/bin/gometalinter \
    --cyclo-over 12 \
    --disable gotype \
    --disable gotypex \
    --vendor \
    ./...
