#!/bin/bash

# Exit on error and ensure cleanup on exit/interrupt
set -e
trap cleanup EXIT INT TERM

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Print header
echo -e "${PURPLE}🏗️  ArchGraph Unified Runner${NC}"
echo -e "${PURPLE}=================================${NC}"

# Target repo path
REPO_PATH="${1:-$(pwd)}"
# Convert to absolute path
REPO_PATH=$(cd "$REPO_PATH" && pwd)

# Find absolute path of the script directory (project root)
PROJECT_ROOT=$(cd "$(dirname "$0")" && pwd)

# Resolve Namespace:
# 1. User argument $2
# 2. Extract from project's .archgraph.yaml
# 3. Fallback to "local-dev"
NAMESPACE="$2"
if [ -z "$NAMESPACE" ] && [ -f "$PROJECT_ROOT/.archgraph.yaml" ]; then
  NAMESPACE=$(grep -E '^namespace:' "$PROJECT_ROOT/.archgraph.yaml" | awk '{print $2}' | tr -d '"' | tr -d "'")
fi
if [ -z "$NAMESPACE" ]; then
  NAMESPACE="local-dev"
fi

echo -e "${BLUE}[1/5] Target Repository:${NC} $REPO_PATH"
echo -e "${BLUE}[1/5] Namespace:${NC} $NAMESPACE"

# Clear any conflicting ports from previous runs
echo -e "${YELLOW}🧹 Clearing any existing processes on ports 8080-8083...${NC}"
PID_TO_KILL=$(lsof -t -i:8080 -i:8081 -i:8082 -i:8083 || true)
if [ -n "$PID_TO_KILL" ]; then
  kill -9 $PID_TO_KILL 2>/dev/null || true
fi

# Generate a temporary sources.json for Zone 2
CONFIG_PATH="/tmp/archgraph_sources.json"
echo -e "${BLUE}[2/5] Generating ingestion config at:${NC} $CONFIG_PATH"
cat <<EOF > "$CONFIG_PATH"
{
  "git": [
    {
      "source_id": "auto-scan",
      "repo_path": "$REPO_PATH",
      "namespace": "$NAMESPACE"
    }
  ],
  "ast_go": [
    {
      "source_id": "auto-scan",
      "root_path": "$REPO_PATH",
      "namespace": "$NAMESPACE",
      "ignore_dirs": ["vendor", "node_modules"]
    }
  ]
}
EOF

# Generate a matching temporary config for the CLI so it queries the same namespace
CLI_CONFIG_PATH="/tmp/archgraph_cli_config.yaml"
echo -e "${BLUE}[2/5] Generating CLI configuration at:${NC} $CLI_CONFIG_PATH"
cat <<EOF > "$CLI_CONFIG_PATH"
version: "1.0"
namespace: "$NAMESPACE"
EOF

# Start the supervisor in the background
echo -e "${BLUE}[3/5] Starting ArchGraph services (Zones 2-5)...${NC}"
# We'll redirect supervisor output to a log file to keep the terminal clean
LOG_FILE="/tmp/archgraph_supervisor.log"
(cd "$PROJECT_ROOT/cmd/archgraph" && go run . -root ../.. -zone2-config "$CONFIG_PATH") > "$LOG_FILE" 2>&1 &
SUPERVISOR_PID=$!

function cleanup() {
  echo -e "\n${YELLOW}🧹 Cleaning up background services...${NC}"
  # Kill supervisor and its children
  if [ -n "$SUPERVISOR_PID" ]; then
    kill -15 "$SUPERVISOR_PID" 2>/dev/null || true
    wait "$SUPERVISOR_PID" 2>/dev/null || true
  fi
  # Clean up temp configs
  rm -f "$CONFIG_PATH"
  rm -f "$CLI_CONFIG_PATH"
  echo -e "${GREEN}✅ Done.${NC}"
}

# Wait for Zone 2 and Zone 4 to be healthy
echo -n -e "${BLUE}[4/5] Waiting for services to become healthy...${NC}"
for i in {1..30}; do
  if curl -s http://localhost:8083/v1/health >/dev/null && curl -s http://localhost:8080/v1/health >/dev/null; then
    echo -e " ${GREEN}Ready!${NC}"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo -e " ${RED}Failed (Timeout).${NC}"
    echo -e "${RED}Check logs in $LOG_FILE${NC}"
    exit 1
  fi
  echo -n "."
  sleep 1
done

# Trigger Ingestion
echo -e "${BLUE}[5/5] Triggering ingestion scan...${NC}"
INGEST_RESP=$(curl -s -X POST http://localhost:8083/v1/runs)
echo -e "${GREEN}Ingestion complete!${NC}"

# Wait a short moment for changes to commit to storage
sleep 1

# Run the CLI to print the graph
echo -e "\n${GREEN}📊 Codebase Dependency Graph:${NC}"
echo -e "${GREEN}---------------------------------${NC}"
(cd "$PROJECT_ROOT/zone6" && go run ./cmd/archgraph-cli -zone4 http://localhost:8080 -zone5 http://localhost:8081 -config "$CLI_CONFIG_PATH" graph)

# Prompt for interactive queries
echo -e "\n${CYAN}💡 You can now query the serving layer in natural language.${NC}"
echo -e "${CYAN}Press Ctrl+C to terminate services and exit.${NC}"
echo -e "${CYAN}Example query: (cd zone6 && go run ./cmd/archgraph-cli -config \"$CLI_CONFIG_PATH\" query \"Which packages depend on main?\")"

# Block and wait
wait $SUPERVISOR_PID
