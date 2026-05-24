#!/bin/bash
# Exit on error
set -e

echo "🏗️ Building supervisor..."
cd cmd/archgraph
go build -tags netgo -ldflags '-s -w' -o ../../app
cd ../..

echo "🏗️ Building Zone 2..."
cd zone2
go build -tags netgo -ldflags '-s -w' -o zone2d ./cmd/zone2d
cd ..

echo "🏗️ Building Zone 3..."
cd zone3
go build -tags netgo -ldflags '-s -w' -o zone3d ./cmd/zone3d
cd ..

echo "🏗️ Building Zone 4..."
cd zone4
go build -tags netgo -ldflags '-s -w' -o zone4d ./cmd/zone4d
cd ..

echo "🏗️ Building Zone 5..."
cd zone5
go build -tags netgo -ldflags '-s -w' -o zone5d ./cmd/zone5d
cd ..

echo "✅ All components compiled successfully!"
