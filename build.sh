#!/bin/bash

echo "Building..."
rm -rf ./build
rm -rf ./site/public
GOOS=windows GOARCH=amd64 go build -o build/win/sitegen.exe
GOOS=linux GOARCH=amd64 go build -o build/linux/sitegen
GOOS=darwin GOARCH=amd64 go build -o build/osx/sitegen

cp -R site/ build/win/
cp -R site/ build/linux/
cp -R site/ build/osx/
cd build/win
zip -rq ../win.zip . -x ".*"
cd ../linux
zip -rq ../linux.zip . -x ".*"
cd ../osx
zip -rq ../osx.zip . -x ".*"
echo "Done"