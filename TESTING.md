# 🧪 Testing Guide

Comprehensive testing scenarios untuk Distributed File Storage System.

---

## Testing Requirements

### Prerequisites

- ✅ Naming Service running di `:8000`
- ✅ Minimal 2 Storage Nodes running (`:9001`, `:9002`)
- ✅ UI Gateway running di `:8080`
- ✅ `curl` installed untuk command-line testing
- ✅ Browser untuk dashboard testing

---

## Test Scenario 1: Basic Upload & Download

### 📤 Upload File

**Step 1: Create test file**
```bash
echo "Hello, Distributed Storage!" > test.txt
```

**Step 2: Upload via API**
```bash
curl -F "file=@test.txt" -F "filename=test.txt" \
  http://localhost:8080/api/upload
```

**Expected Response:**
```json
{
  "fileId": "abc123-...",
  "filename": "test.txt",
  "size": 29,
  "checksum": "sha256:...",
  "uploaded": ["node-a", "node-b"],
  "commit": {
    "state": "AVAILABLE"
  }
}
```

**✅ Success Criteria:**
- Response status: 200 OK
- `uploaded` array contains 2 node IDs
- `commit.state` is "AVAILABLE"

---

### 📥 Download File

**Step 3: Lookup file locations**
```bash
curl http://localhost:8080/api/lookup?fileId=abc123-...
```

**Expected Response:**
```json
[
  {
    "nodeId": "node-a",
    "url": "http://localhost:9001"
  },
  {
    "nodeId": "node-b",
    "url": "http://localhost:9002"
  }
]
```

**Step 4: Download from node-a**
```bash
curl "http://localhost:8080/api/download?fileId=abc123-...&nodeUrl=http://localhost:9001" \
  -o downloaded.txt
```

**Step 5: Verify content**
```bash
cat downloaded.txt
# Output: Hello, Distributed Storage!

# Verify checksum
sha256sum downloaded.txt
```

**✅ Success Criteria:**
- File downloaded successfully
- Content matches original
- Checksum matches

---

### 📊 Verify Metrics

**Step 6: Check system metrics**
```bash
curl http://localhost:8080/api/metrics
```

**Expected Response:**
```json
{
  "totalFiles": 1,
  "totalNodes": 2,
  "totalSizeBytes": 29,
  "nodes": {
    "healthy": 2,
    "suspect": 0,
    "down": 0
  },
  "storage": {
    "capacity": 2147483648,
    "used": 58,
    "free": 2147483590
  },
  "filesByState": {
    "AVAILABLE": 1
  }
}
```

**✅ Success Criteria:**
- `totalFiles` = 1
- `nodes.healthy` = 2
- `filesByState.AVAILABLE` = 1

---

## Test Scenario 2: Node Failure & Recovery

### 🔴 Simulate Node Failure

**Step 1: Upload test file**
```bash
curl -F "file=@test.txt" -F "filename=failure-test.txt" \
  http://localhost:8080/api/upload
```

Save the `fileId` from response.

**Step 2: Verify 2 replicas exist**
```bash
curl http://localhost:8080/api/lookup?fileId={fileId}
```

Should return 2 nodes.

**Step 3: Stop node-b**
```bash
# In terminal running node-b, press Ctrl+C
```

**Step 4: Wait 25 seconds (for DOWN detection)**
```bash
sleep 25
```

**Step 5: Check node status**
```bash
curl http://localhost:8080/api/nodes
```

**Expected Response:**
```json
[
  {
    "nodeId": "node-a",
    "status": "HEALTHY",
    ...
  },
  {
    "nodeId": "node-b",
    "status": "DOWN",
    ...
  }
]
```

**Step 6: Verify file still accessible from node-a**
```bash
curl "http://localhost:8080/api/download?fileId={fileId}&nodeUrl=http://localhost:9001" \
  -o recovered.txt

cat recovered.txt
# Should show file content
```

**✅ Success Criteria:**
- Node-b status changes to "DOWN"
- File still downloadable from node-a
- Metrics show 1 healthy, 1 down node

---

### 🟢 Node Recovery

**Step 7: Restart node-b**
```bash
cd storage_node
NODE_ID=node-b PORT=9002 DATA_DIR=./data_b go run main.go
```

**Step 8: Wait for heartbeat (5-10 seconds)**
```bash
sleep 10
```

**Step 9: Verify node-b is HEALTHY again**
```bash
curl http://localhost:8080/api/nodes | grep node-b
```

**Expected:** `"status": "HEALTHY"`

**✅ Success Criteria:**
- Node-b auto-registers on startup
- Status returns to HEALTHY within 10 seconds
- Files previously on node-b are still accessible

---

## Test Scenario 3: Auto-Healing

### 🔧 Test Auto-Healing Mechanism

**Step 1: Upload file**
```bash
curl -F "file=@test.txt" -F "filename=healing-test.txt" \
  http://localhost:8080/api/upload
```

Save `fileId`.

**Step 2: Manually delete file from node-a storage**
```bash
# Find file path
curl http://localhost:9001/list

# Delete file physically
rm storage_node/data_a/*/abc123...
# (Adjust path based on file ID)
```

**Step 3: Verify file is missing on node-a**
```bash
curl "http://localhost:9001/has?fileId={fileId}"
```

**Expected:** `{"exists": false}`

**Step 4: Wait for auto-healing (30-60 seconds)**
```bash
# Watch naming service logs for:
# [AUTO-HEAL] File abc123... has only 1 healthy replicas, need 2
# [AUTO-HEAL] Added replica candidate: node-c for file abc123...
```

**Step 5: Check file info**
```bash
curl http://localhost:8000/file-info/{fileId}
```

**Expected Response:**
```json
{
  "state": "DEGRADED",
  "replicas": [
    {
      "nodeId": "node-a",
      "status": "MISSING"
    },
    {
      "nodeId": "node-b",
      "status": "READY"
    },
    {
      "nodeId": "node-c",
      "status": "MISSING"
    }
  ]
}
```

**✅ Success Criteria:**
- File state changes to "DEGRADED"
- New replica candidate added
- Healing activity logged

---

## Test Scenario 4: Multiple Files Upload

### 📚 Bulk Upload Test

**Step 1: Create multiple test files**
```bash
for i in {1..10}; do
  echo "Test file $i" > test_$i.txt
done
```

**Step 2: Upload all files**
```bash
for i in {1..10}; do
  curl -F "file=@test_$i.txt" -F "filename=test_$i.txt" \
    http://localhost:8080/api/upload
  sleep 1
done
```

**Step 3: Verify all files uploaded**
```bash
curl http://localhost:8080/api/files
```

**Expected:** Array of 10+ files

**Step 4: Check load balancing**
```bash
curl http://localhost:8080/api/nodes
```

**Expected:** Both nodes should have similar `usedBytes`

**✅ Success Criteria:**
- All 10 files uploaded successfully
- Load distributed evenly across nodes
- All files in AVAILABLE state

---

## Test Scenario 5: Large File Upload

### 📦 Large File Test

**Step 1: Create 10MB test file**
```bash
dd if=/dev/urandom of=large.bin bs=1M count=10
```

**Step 2: Upload large file**
```bash
curl -F "file=@large.bin" -F "filename=large.bin" \
  http://localhost:8080/api/upload
```

**Step 3: Verify upload**
```bash
# Should complete successfully
# Check response for uploaded nodes
```

**Step 4: Download and verify**
```bash
curl "http://localhost:8080/api/download?fileId={fileId}&nodeUrl=http://localhost:9001" \
  -o large_downloaded.bin

# Verify size
ls -lh large_downloaded.bin
# Should be ~10MB

# Verify checksum
sha256sum large.bin large_downloaded.bin
# Checksums should match
```

**✅ Success Criteria:**
- Large file uploads without errors
- Download completes successfully
- Checksums match

---

## Test Scenario 6: Dashboard UI Testing

### 🖥️ Dashboard Functionality

**Step 1: Open dashboard**
```
http://localhost:8080/dashboard
```

**Step 2: Verify metrics display**
- Total Files should be > 0
- Storage Nodes should show 2/2 or 1/2 (if one down)
- System Status should be OPERATIONAL or DEGRADED

**Step 3: Test file upload via UI**
1. Click "Click to select file"
2. Choose a file
3. Click "Upload File"
4. Verify success message appears

**Step 4: Verify files table**
- Should show all uploaded files
- State badges should display correctly
- Replica count should be accurate

**Step 5: Verify nodes table**
- Should show all registered nodes
- Status badges (HEALTHY/DOWN) should be accurate
- Capacity/Used/Free should display correctly

**Step 6: Test file deletion**
1. Click "Delete" on a file
2. Confirm deletion
3. Verify file disappears from list
4. Verify metrics update

**Step 7: Test auto-refresh**
- Leave dashboard open
- Stop a storage node
- Within 5-10 seconds, dashboard should update to show node DOWN

**✅ Success Criteria:**
- Dashboard loads without errors
- Metrics display accurately
- Upload works via UI
- File deletion works
- Auto-refresh updates data

---

## Test Scenario 7: Error Handling

### ❌ Error Cases

**Test 1: Upload with insufficient nodes**
```bash
# Stop all storage nodes except one
# Try to upload (requires 2 nodes)
curl -F "file=@test.txt" -F "filename=test.txt" \
  http://localhost:8080/api/upload
```

**Expected:** 
- Status: 400 or 409
- Error message about insufficient nodes

**Test 2: Download non-existent file**
```bash
curl http://localhost:8080/api/lookup?fileId=nonexistent
```

**Expected:**
- Status: 404
- Error message: "not found"

**Test 3: Delete non-existent file**
```bash
curl -X POST http://localhost:8080/api/delete \
  -H "Content-Type: application/json" \
  -d '{"fileId":"nonexistent"}'
```

**Expected:**
- Status: 404
- Error message: "file not found"

**✅ Success Criteria:**
- Proper HTTP status codes returned
- Clear error messages provided
- System remains stable after errors

---

## Performance Testing

### ⚡ Load Test

**Test 1: Concurrent uploads**
```bash
# Install apache bench (optional)
# apt-get install apache2-utils

# Create test file
echo "Load test" > load.txt

# Run concurrent uploads
for i in {1..50}; do
  curl -F "file=@load.txt" -F "filename=load_$i.txt" \
    http://localhost:8080/api/upload &
done
wait
```

**Expected:**
- All uploads complete successfully
- No errors or timeouts
- System remains responsive

**✅ Success Criteria:**
- 90%+ success rate
- Average response time < 2 seconds per file
- No crashes or hangs

---

## Test Results Template

### Test Execution Report

| Scenario | Status | Notes |
|----------|--------|-------|
| Basic Upload/Download | ✅ PASS | All files uploaded and downloaded successfully |
| Node Failure | ✅ PASS | System continued operating with 1 node down |
| Node Recovery | ✅ PASS | Node rejoined cluster within 10s |
| Auto-Healing | ✅ PASS | Degraded files detected, new replicas allocated |
| Multiple Files | ✅ PASS | 10 files uploaded, load balanced |
| Large File | ✅ PASS | 10MB file uploaded/downloaded, checksums match |
| Dashboard | ✅ PASS | All UI features working |
| Error Handling | ✅ PASS | Proper error codes and messages |

---

## Logging & Debugging

### Enable Detailed Logging

Monitor naming service logs:
```bash
cd naming_service
go run main.go 2>&1 | tee naming.log
```

Monitor storage node logs:
```bash
cd storage_node
NODE_ID=node-a PORT=9001 DATA_DIR=./data_a go run main.go 2>&1 | tee node-a.log
```

### Important Log Messages

**Auto-Healing:**
```
[AUTO-HEAL] File abc123 (test.txt) has only 1 healthy replicas, need 2
[AUTO-HEAL] Added replica candidate: node-c for file abc123
```

**Node Status Changes:**
```
# Heartbeat received
POST /heartbeat 5ms

# Node registered
POST /register-node 2ms
```

---

## Continuous Testing

### Automated Test Script

Create `test_all.sh`:
```bash
#!/bin/bash

echo "🧪 Running all tests..."

# Test 1: Upload
echo "Test 1: Upload file"
RESPONSE=$(curl -s -F "file=@test.txt" -F "filename=test.txt" http://localhost:8080/api/upload)
FILE_ID=$(echo $RESPONSE | grep -o '"fileId":"[^"]*"' | cut -d'"' -f4)

if [ -z "$FILE_ID" ]; then
  echo "❌ FAIL: Upload failed"
  exit 1
fi
echo "✅ PASS: File uploaded, ID=$FILE_ID"

# Test 2: Download
echo "Test 2: Download file"
curl -s "http://localhost:8080/api/download?fileId=$FILE_ID&nodeUrl=http://localhost:9001" -o test_downloaded.txt
if [ -f test_downloaded.txt ]; then
  echo "✅ PASS: File downloaded"
else
  echo "❌ FAIL: Download failed"
  exit 1
fi

# Test 3: Metrics
echo "Test 3: Check metrics"
METRICS=$(curl -s http://localhost:8080/api/metrics)
HEALTHY=$(echo $METRICS | grep -o '"healthy":[0-9]*' | cut -d':' -f2)
if [ "$HEALTHY" -ge "1" ]; then
  echo "✅ PASS: Metrics OK, $HEALTHY healthy nodes"
else
  echo "❌ FAIL: No healthy nodes"
  exit 1
fi

echo "🎉 All tests passed!"
```

Run:
```bash
chmod +x test_all.sh
./test_all.sh
```

---

## Troubleshooting

### Common Issues

**Issue: Cannot allocate file - insufficient nodes**
- **Cause:** Less than 2 storage nodes running
- **Fix:** Start more storage nodes

**Issue: Node shows as DOWN but is running**
- **Cause:** Heartbeat timeout
- **Fix:** Check network connectivity, restart node

**Issue: File upload succeeds but state is PARTIAL**
- **Cause:** Upload to some nodes failed
- **Fix:** Check storage node logs, ensure nodes have free space

**Issue: Auto-healing not working**
- **Cause:** No candidate nodes available
- **Fix:** Start more storage nodes, check capacity

---

## Next Steps

After testing:
1. Document any failures or issues
2. Verify all test scenarios pass
3. Take screenshots of dashboard
4. Generate test report
5. Include in project submission

**Test Status: READY FOR SUBMISSION** ✅
