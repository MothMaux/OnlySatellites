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
    export CGO_CFLAGS="-D_LARGEFILE64_SOURCE"
    export CGO_LDFLAGS="-lstdc++"
    echo "Building in release mode with large file support..."
fi
if [ $1 = "debug" ]; then
    echo "Building in debug mode... (this does nothing yet)"
fi
if [ $1 = "experimental" ] && [ $GREEN = true ]; then
    export GOEXPERIMENT=greenteagc
    echo "greenteagc enabled for this build"
fi

#export GOEXPERIMENT=greenteagc #uncomment to use golang experimental garbage collector. Requires golang 1.25 or higher.

go run com/comptime/minify.go

echo "Web files minified successfully!"
go build -o OnlySats main.go
if [ $? -ne 0 ]; then
    echo "Failed to build main application"
    exit 1
fi
echo "Build completed successfully!"
rm -rf web
echo "Temporary files cleaned up!"
