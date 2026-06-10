#!/bin/bash

echo "Building OnlySats on $(go version)"

# Set Go environment variables
export GOOS=$(go env GOOS)
export GOARCH=amd64
export CGO_ENABLED=1

if [ $GOVERSION >= go1.25 ]; then
    GREEN=true
    echo "using $(go version), greenteagc is available, you can enable it by running './build.sh experimental'"
    
fi

if [ $1 = "release" ]; then
    echo "Building in release mode with large file support..."
fi
if [ $1 = "experimental" ] && [ $GREEN = true ]; then
    export GOEXPERIMENT=greenteagc
    echo "greenteagc enabled for this build"
fi

go run com/comptime/minify.go
echo "Temp files created, building application. This may take a moment..."
go build -o OnlySats main.go
if [ $? -ne 0 ]; then
    echo "Failed to build main application"
    exit 1
fi
echo "Build completed successfully!"
rm -rf web
echo "Temporary files cleaned up!"
