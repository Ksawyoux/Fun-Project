#!/bin/bash
# Exit on error
set -e

echo "🏗️ Building supervisor..."
go build -tags netgo -ldflags '-s -w' -o app ./cmd/archgraph

echo "🏗️ Building Zone 2..."
go build -tags netgo -ldflags '-s -w' -o zone2/zone2d ./zone2/cmd/zone2d

echo "🏗️ Building Zone 3..."
go build -tags netgo -ldflags '-s -w' -o zone3/zone3d ./zone3/cmd/zone3d

echo "🏗️ Building Zone 4..."
go build -tags netgo -ldflags '-s -w' -o zone4/zone4d ./zone4/cmd/zone4d

echo "🏗️ Building Zone 5..."
go build -tags netgo -ldflags '-s -w' -o zone5/zone5d ./zone5/cmd/zone5d

echo "✅ All components compiled successfully!"

