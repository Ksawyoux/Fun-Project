#!/bin/bash
# Exit on error
set -e

echo "🏗️ Building supervisor..."
go build -tags netgo -ldflags '-s -w' -o cmd/archgraph/archgraph ./cmd/archgraph

echo "🏗️ Building Ingestion Subsystem..."
go build -tags netgo -ldflags '-s -w' -o ingestion/ingestiond ./ingestion/cmd/ingestiond

echo "🏗️ Building Processing Pipeline..."
go build -tags netgo -ldflags '-s -w' -o pipeline/pipelined ./pipeline/cmd/pipelined

echo "🏗️ Building Graph Storage..."
go build -tags netgo -ldflags '-s -w' -o storage/storaged ./storage/cmd/storaged

echo "🏗️ Building Serving Layer..."
go build -tags netgo -ldflags '-s -w' -o serving/servingd ./serving/cmd/servingd

echo "🏗️ Building Consumer Interfaces (CLI)..."
go build -tags netgo -ldflags '-s -w' -o interfaces/archgraph-cli ./interfaces/cmd/archgraph-cli

echo "✅ All components compiled successfully!"

