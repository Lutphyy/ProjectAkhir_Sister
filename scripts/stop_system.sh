#!/bin/bash

# Distributed Storage System - Stop Script

echo "🛑 Stopping Distributed File Storage System..."
echo ""

# Function to stop process
stop_service() {
    SERVICE_NAME=$1
    PID_FILE=".pids/$2.pid"
    
    if [ -f "$PID_FILE" ]; then
        PID=$(cat "$PID_FILE")
        if ps -p $PID > /dev/null 2>&1; then
            kill $PID
            echo "✅ Stopped $SERVICE_NAME (PID: $PID)"
            rm "$PID_FILE"
        else
            echo "⚠️  $SERVICE_NAME not running"
            rm "$PID_FILE"
        fi
    else
        echo "⚠️  No PID file for $SERVICE_NAME"
    fi
}

# Stop all services
stop_service "UI Gateway" "gateway"
stop_service "Storage Node B" "node-b"
stop_service "Storage Node A" "node-a"
stop_service "Naming Service" "naming"

# Clean up PID directory
if [ -d ".pids" ] && [ -z "$(ls -A .pids)" ]; then
    rmdir .pids
fi

echo ""
echo "✅ All services stopped"
echo ""
