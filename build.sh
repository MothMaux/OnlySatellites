#!/bin/bash

echo "Building OnlySats Go application..."

# Set Go environment variables
export GOOS=$(go env GOOS)
export GOARCH=amd64
export CGO_ENABLED=1
#uncomment to use golang experimental garbage collector. Requires golang 1.25 or higher.
#export GOEXPERIMENT=greenteagc

echo "Building main application..."
go build -o OnlySats main.go
if [ $? -ne 0 ]; then
    echo "Failed to build main application"
    exit 1
fi

echo "Build completed successfully!"
