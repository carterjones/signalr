#!/bin/bash
set -euxo pipefail

# Run unit tests and show coverage.
for dir in . hubs; do
    pushd $dir
    go test -coverprofile=c.out
    popd
done
