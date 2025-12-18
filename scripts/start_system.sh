#!/bin/bash
set -euo pipefail

mkdir -p storage_node/data_a storage_node/data_b

(cd naming_service && go run main.go) &
NS_PID=$!

(cd storage_node && NODE_ID=node-a PORT=9001 DATA_DIR=./data_a NAMING_URL=http://localhost:8000 CAPACITY_BYTES=1073741824 go run main.go) &
NODE_A_PID=$!

(cd storage_node && NODE_ID=node-b PORT=9002 DATA_DIR=./data_b NAMING_URL=http://localhost:8000 CAPACITY_BYTES=1073741824 go run main.go) &
NODE_B_PID=$!

(cd ui_gateway && NAMING_URL=http://localhost:8000 ADDR=:8080 go run main.go) &
GW_PID=$!

trap "kill $GW_PID $NODE_A_PID $NODE_B_PID $NS_PID 2>/dev/null || true; exit 0" INT TERM
wait

# Distributed Storage System - Startup Script
# This script starts all components of the system

echo "🚀 Starting Distributed File Storage System..."
echo ""

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to check if port is in use
check_port() {
    if lsof -Pi :$1 -sTCP:LISTEN -t >/dev/null 2>&1 ; then
        echo -e "${YELLOW}⚠️  Port $1 is already in use${NC}"
        return 1
    else
        return 0
    fi
}

# Check prerequisites
echo -e "${BLUE}📋 Checking prerequisites...${NC}"

if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.19+"
    exit 1
fi

echo "✅ Go version: $(go version)"
echo ""

# Check ports
echo -e "${BLUE}🔍 Checking ports...${NC}"
check_port 8000 || exit 1
check_port 8080 || exit 1
check_port 9001 || exit 1
check_port 9002 || exit 1
echo "✅ All ports available"
echo ""

# Start Naming Service
echo -e "${GREEN}🏗️  Starting Naming Service (port 8000)...${NC}"
cd naming_service
go run main.go > ../logs/naming.log 2>&1 &
NAMING_PID=$!
echo "   PID: $NAMING_PID"
cd ..
sleep 2

# Start Storage Node A
echo -e "${GREEN}💾 Starting Storage Node A (port 9001)...${NC}"
cd storage_node
NODE_ID=node-a PORT=9001 DATA_DIR=./data_a go run main.go > ../logs/node-a.log 2>&1 &
NODE_A_PID=$!
echo "   PID: $NODE_A_PID"
cd ..
sleep 2

# Start Storage Node B
echo -e "${GREEN}💾 Starting Storage Node B (port 9002)...${NC}"
cd storage_node
NODE_ID=node-b PORT=9002 DATA_DIR=./data_b go run main.go > ../logs/node-b.log 2>&1 &
NODE_B_PID=$!
echo "   PID: $NODE_B_PID"
cd ..
sleep 2

# Start UI Gateway
echo -e "${GREEN}🌐 Starting UI Gateway (port 8080)...${NC}"
cd ui_gateway
go run main.go > ../logs/gateway.log 2>&1 &
GATEWAY_PID=$!
echo "   PID: $GATEWAY_PID"
cd ..
sleep 2

# Save PIDs
mkdir -p .pids
echo $NAMING_PID > .pids/naming.pid
echo $NODE_A_PID > .pids/node-a.pid
echo $NODE_B_PID > .pids/node-b.pid
echo $GATEWAY_PID > .pids/gateway.pid

echo ""
echo -e "${GREEN}✅ All services started!${NC}"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📊 Access Points:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  🎯 Admin Dashboard:  http://localhost:8080/dashboard"
echo "  📤 Upload UI:        http://localhost:8080/"
echo "  🔧 Naming Service:   http://localhost:8000/metrics"
echo "  💾 Storage Node A:   http://localhost:9001/health"
echo "  💾 Storage Node B:   http://localhost:9002/health"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📝 Logs:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  tail -f logs/naming.log"
echo "  tail -f logs/node-a.log"
echo "  tail -f logs/node-b.log"
echo "  tail -f logs/gateway.log"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "💡 To stop all services, run: ./scripts/stop_system.sh"
echo ""
