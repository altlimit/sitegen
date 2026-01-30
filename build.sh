#!/bin/bash
if [ -z "$VERSION" ]; then
    VERSION="dev"
fi
echo "Building version $VERSION..."
rm -rf ./build
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o build/win/sitegen.exe
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o build/linux/sitegen
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.version=$VERSION" -o build/darwin/sitegen
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "-X main.version=$VERSION" -o build/darwin-arm64/sitegen
cd build/win
zip -rq ../win.zip . -x ".*"
cd ../linux
tar -czf ../linux.tgz sitegen
cd ../darwin
tar -czf ../darwin.tgz sitegen
cd ../darwin-arm64
tar -czf ../darwin-arm64.tgz sitegen
cd ..
rm -rf darwin
rm -rf darwin-arm64
rm -rf linux
rm -rf win
echo "Done"