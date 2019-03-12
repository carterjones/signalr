#!/bin/bash

if [[ "${TASK}" == "Run linters" ]]; then
    make lint
    exit $?
elif [[ "${TASK}" == "Run tests" ]]; then
    make test
    exit $?
elif [[ "${TASK}" == "Code coverage" ]]; then
    curl -L https://codeclimate.com/downloads/test-reporter/test-reporter-latest-linux-amd64 > ./cc-test-reporter
    chmod +x ./cc-test-reporter
    ./cc-test-reporter before-build
    make cover
    ./cc-test-reporter after-build --exit-code $?
    bash <(curl -s https://codecov.io/bash)
    exit $?
else
    echo "Unsupported task."
    exit 1
fi
