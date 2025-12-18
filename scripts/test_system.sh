#!/bin/bash

# Automated Testing Script for Distributed Storage System

echo "🧪 Running Automated Tests..."
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Test counters
PASSED=0
FAILED=0

# Test function
run_test() {
    TEST_NAME=$1
    shift
    COMMAND="$@"
    
    echo -n "  Testing: $TEST_NAME... "
    
    RESULT=$(eval "$COMMAND" 2>&1)
    EXIT_CODE=$?
    
    if [ $EXIT_CODE -eq 0 ]; then
        echo -e "${GREEN}✅ PASS${NC}"
        ((PASSED++))
        return 0
    else
        echo -e "${RED}❌ FAIL${NC}"
        echo "     Error: $RESULT"
        ((FAILED++))
        return 1
    fi
}

# Wait for services to be ready
echo "⏳ Waiting for services to be ready..."
sleep 5

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📡 API Connectivity Tests"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

run_test "Naming Service health" "curl -s http://localhost:8000/metrics >/dev/null"
run_test "Storage Node A health" "curl -s http://localhost:9001/health >/dev/null"
run_test "Storage Node B health" "curl -s http://localhost:9002/health >/dev/null"
run_test "UI Gateway health" "curl -s http://localhost:8080/ >/dev/null"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📤 File Upload Tests"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Create test file
echo "Test content $(date)" > /tmp/test_upload.txt

# Test upload
UPLOAD_RESULT=$(curl -s -F "file=@/tmp/test_upload.txt" -F "filename=test_$(date +%s).txt" http://localhost:8080/api/upload)
FILE_ID=$(echo "$UPLOAD_RESULT" | grep -o '"fileId":"[^"]*"' | cut -d'"' -f4)

if [ -n "$FILE_ID" ]; then
    echo -e "  ${GREEN}✅ PASS${NC} File upload"
    echo "     File ID: $FILE_ID"
    ((PASSED++))
else
    echo -e "  ${RED}❌ FAIL${NC} File upload"
    echo "     Response: $UPLOAD_RESULT"
    ((FAILED++))
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📥 File Download Tests"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

if [ -n "$FILE_ID" ]; then
    # Test lookup
    LOOKUP_RESULT=$(curl -s "http://localhost:8080/api/lookup?fileId=$FILE_ID")
    NODE_COUNT=$(echo "$LOOKUP_RESULT" | grep -o '"nodeId"' | wc -l)
    
    if [ "$NODE_COUNT" -ge 1 ]; then
        echo -e "  ${GREEN}✅ PASS${NC} File lookup ($NODE_COUNT replicas found)"
        ((PASSED++))
        
        # Test download
        curl -s "http://localhost:8080/api/download?fileId=$FILE_ID&nodeUrl=http://localhost:9001" -o /tmp/test_downloaded.txt
        
        if [ -f /tmp/test_downloaded.txt ] && [ -s /tmp/test_downloaded.txt ]; then
            echo -e "  ${GREEN}✅ PASS${NC} File download"
            ((PASSED++))
        else
            echo -e "  ${RED}❌ FAIL${NC} File download"
            ((FAILED++))
        fi
    else
        echo -e "  ${RED}❌ FAIL${NC} File lookup"
        ((FAILED++))
    fi
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📊 Metrics Tests"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

run_test "Get system metrics" "curl -s http://localhost:8080/api/metrics | grep totalFiles"
run_test "List files" "curl -s http://localhost:8080/api/files"
run_test "List nodes" "curl -s http://localhost:8080/api/nodes"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📈 Load Test"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

echo "  Uploading 5 files concurrently..."
for i in {1..5}; do
    echo "Load test file $i" > /tmp/load_$i.txt
    curl -s -F "file=@/tmp/load_$i.txt" -F "filename=load_$i.txt" http://localhost:8080/api/upload > /dev/null &
done
wait

TOTAL_FILES=$(curl -s http://localhost:8080/api/metrics | grep -o '"totalFiles":[0-9]*' | cut -d':' -f2)
if [ "$TOTAL_FILES" -ge 5 ]; then
    echo -e "  ${GREEN}✅ PASS${NC} Load test (Total files: $TOTAL_FILES)"
    ((PASSED++))
else
    echo -e "  ${RED}❌ FAIL${NC} Load test"
    ((FAILED++))
fi

# Cleanup
rm -f /tmp/test_*.txt /tmp/load_*.txt

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  📝 Test Summary"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo -e "  ${GREEN}Passed:${NC} $PASSED"
echo -e "  ${RED}Failed:${NC} $FAILED"
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "  ${GREEN}🎉 All tests passed!${NC}"
    echo ""
    exit 0
else
    echo -e "  ${YELLOW}⚠️  Some tests failed${NC}"
    echo ""
    exit 1
fi
