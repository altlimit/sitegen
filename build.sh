#!/bin/bash
echo "Building..."
rm -rf ./build
GOOS=windows GOARCH=amd64 go build -o build/win/sitegen.exe
GOOS=linux GOARCH=amd64 go build -o build/linux/sitegen
GOOS=darwin GOARCH=amd64 go build -o build/darwin/sitegen
GOOS=darwin GOARCH=arm64 go build -o build/darwin-arm64/sitegen
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