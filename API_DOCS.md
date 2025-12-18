# 📡 API Documentation

Complete REST API reference for Distributed File Storage System.

---

## Naming Service API (`:8000`)

### 1. Register Node

Register storage node to naming service.

**Endpoint:** `POST /register-node`

**Request:**
```json
{
  "nodeId": "node-a",
  "url": "http://localhost:9001",
  "capacityBytes": 1073741824,
  "zone": "zone-1",
  "tags": ["ssd", "fast"]
}
```

**Response:**
```json
{
  "ok": true
}
```

---

### 2. Heartbeat

Node health check and usage update.

**Endpoint:** `POST /heartbeat`

**Request:**
```json
{
  "nodeId": "node-a",
  "usedBytes": 524288000
}
```

**Response:**
```json
{
  "ok": true,
  "status": "HEALTHY"
}
```

---

### 3. Allocate File

Allocate file and get replica node assignments.

**Endpoint:** `POST /allocate`

**Request:**
```json
{
  "filename": "document.pdf",
  "size": 1048576,
  "checksum": "sha256:abc123...",
  "contentType": "application/pdf"
}
```

**Response:**
```json
{
  "fileId": "f7a3b2c1-...",
  "replicas": [
    {
      "nodeId": "node-a",
      "url": "http://localhost:9001"
    },
    {
      "nodeId": "node-b",
      "url": "http://localhost:9002"
    }
  ]
}
```

---

### 4. Commit Upload

Commit upload result and update file state.

**Endpoint:** `POST /commit`

**Request:**
```json
{
  "fileId": "f7a3b2c1-...",
  "uploaded": ["node-a", "node-b"]
}
```

**Response:**
```json
{
  "state": "AVAILABLE"
}
```

---

### 5. Lookup File

Get file replica locations.

**Endpoint:** `GET /lookup/{fileId}`

**Response:**
```json
[
  {
    "NodeID": "node-a",
    "URL": "http://localhost:9001"
  },
  {
    "NodeID": "node-b",
    "URL": "http://localhost:9002"
  }
]
```

> Healthy nodes are returned first.

---

### 6. System Metrics

Get system-wide metrics.

**Endpoint:** `GET /metrics`

**Response:**
```json
{
  "totalFiles": 42,
  "totalNodes": 2,
  "totalSizeBytes": 524288000,
  "nodes": {
    "healthy": 2,
    "suspect": 0,
    "down": 0
  },
  "storage": {
    "capacity": 2147483648,
    "used": 524288000,
    "free": 1623195648
  },
  "filesByState": {
    "AVAILABLE": 40,
    "DEGRADED": 2,
    "PARTIAL": 0
  }
}
```

---

### 7. List Files

List all files in system.

**Endpoint:** `GET /list-files`

**Response:**
```json
[
  {
    "fileId": "f7a3b2c1-...",
    "filename": "document.pdf",
    "size": 1048576,
    "state": "AVAILABLE",
    "replicaCount": 2,
    "createdAt": "2025-12-04T00:00:00Z"
  }
]
```

---

### 8. List Nodes

List all registered storage nodes.

**Endpoint:** `GET /list-nodes`

**Response:**
```json
[
  {
    "nodeId": "node-a",
    "url": "http://localhost:9001",
    "status": "HEALTHY",
    "capacityBytes": 1073741824,
    "usedBytes": 262144000,
    "freeBytes": 811597824,
    "loadFactor": 0.24,
    "lastSeenAt": "2025-12-04T00:00:00Z"
  }
]
```

---

### 9. File Info

Get detailed file information.

**Endpoint:** `GET /file-info/{fileId}`

**Response:**
```json
{
  "fileId": "f7a3b2c1-...",
  "filename": "document.pdf",
  "size": 1048576,
  "checksum": "sha256:abc123...",
  "contentType": "application/pdf",
  "version": 1,
  "state": "AVAILABLE",
  "replicas": [
    {
      "nodeId": "node-a",
      "url": "http://localhost:9001",
      "status": "READY",
      "lastVerifiedAt": "2025-12-04T00:00:00Z"
    }
  ],
  "createdAt": "2025-12-04T00:00:00Z",
  "updatedAt": "2025-12-04T00:00:00Z"
}
```

---

### 10. Delete File

Soft delete file.

**Endpoint:** `POST /delete-file`

**Request:**
```json
{
  "fileId": "f7a3b2c1-..."
}
```

**Response:**
```json
{
  "deleted": true,
  "fileId": "f7a3b2c1-..."
}
```

---

### 11. Report Missing

Report missing replica.

**Endpoint:** `POST /report-missing`

**Request:**
```json
{
  "fileId": "f7a3b2c1-...",
  "nodeId": "node-a"
}
```

**Response:**
```json
{
  "accepted": true,
  "state": "DEGRADED"
}
```

---

## Storage Node API (`:9001`, `:9002`)

### 1. Upload File

Upload file to storage node.

**Endpoint:** `POST /upload`

**Request:** `multipart/form-data`
- `fileId`: File identifier
- `file`: File binary

**Response:**
```json
{
  "ok": true,
  "fileId": "f7a3b2c1-...",
  "size": 1048576,
  "checksum": "sha256:abc123...",
  "name": "document.pdf"
}
```

---

### 2. Download File

Download file from storage node.

**Endpoint:** `GET /download/{fileId}`

**Response:** File binary with `Content-Type` header

---

### 3. Check File Exists

Check if file exists on node.

**Endpoint:** `GET /has?fileId={fileId}`

**Response:**
```json
{
  "exists": true
}
```

---

### 4. Node Health

Get node health and metrics.

**Endpoint:** `GET /health`

**Response:**
```json
{
  "nodeId": "node-a",
  "status": "HEALTHY",
  "usedBytes": 262144000,
  "capacityBytes": 1073741824,
  "freeBytes": 811597824,
  "dataDir": "./data_a"
}
```

---

### 5. List Files

List all files on node.

**Endpoint:** `GET /list`

**Response:**
```json
{
  "files": [
    {
      "fileId": "f7a3b2c1-...",
      "size": 1048576
    }
  ],
  "count": 1
}
```

---

### 6. Verify Checksum

Verify file integrity.

**Endpoint:** `POST /verify`

**Request:**
```json
{
  "fileId": "f7a3b2c1-...",
  "checksum": "sha256:abc123..."
}
```

**Response:**
```json
{
  "fileId": "f7a3b2c1-...",
  "expectedChecksum": "sha256:abc123...",
  "actualChecksum": "sha256:abc123...",
  "verified": true
}
```

---

## UI Gateway API (`:8080`)

### 1. Upload File

Upload file through gateway (handles allocation & replication).

**Endpoint:** `POST /api/upload`

**Request:** `multipart/form-data`
- `filename`: Original filename
- `file`: File binary

**Response:**
```json
{
  "fileId": "f7a3b2c1-...",
  "filename": "document.pdf",
  "size": 1048576,
  "checksum": "sha256:abc123...",
  "uploaded": ["node-a", "node-b"],
  "commit": {
    "state": "AVAILABLE"
  }
}
```

**Error Response (Insufficient replicas):**
```json
{
  "error": "not enough replicas uploaded",
  "detail": "uploaded 1, required 2"
}
```

---

### 2. Lookup File

Lookup file replica locations.

**Endpoint:** `GET /api/lookup?fileId={fileId}`

**Response:**
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

---

### 3. Download File (Proxy)

Proxy download from storage node.

**Endpoint:** `GET /api/download?fileId={fileId}&nodeUrl={nodeUrl}`

**Response:** File binary

---

### 4. List Files

List all files in system.

**Endpoint:** `GET /api/files`

**Response:** Same as Naming Service `/list-files`

---

### 5. List Nodes

List all storage nodes.

**Endpoint:** `GET /api/nodes`

**Response:** Same as Naming Service `/list-nodes`

---

### 6. System Metrics

Get system metrics.

**Endpoint:** `GET /api/metrics`

**Response:** Same as Naming Service `/metrics`

---

### 7. Delete File

Delete file from system.

**Endpoint:** `POST /api/delete`

**Request:**
```json
{
  "fileId": "f7a3b2c1-..."
}
```

**Response:**
```json
{
  "deleted": true,
  "fileId": "f7a3b2c1-..."
}
```

---

## Error Codes

| Status Code | Description |
|-------------|-------------|
| 200 | Success |
| 400 | Bad Request (invalid payload) |
| 404 | Not Found (file/node not found) |
| 409 | Conflict (insufficient nodes for replication) |
| 500 | Internal Server Error |
| 502 | Bad Gateway (node communication failed) |

---

## Common Workflows

### Complete Upload Flow

```bash
# 1. Client uploads to gateway
curl -F "file=@document.pdf" -F "filename=document.pdf" \
  http://localhost:8080/api/upload

# Gateway internally:
# - Calculates checksum
# - Calls /allocate to get nodes
# - Uploads to each replica node
# - Calls /commit with results

# 2. Response
{
  "fileId": "abc123",
  "uploaded": ["node-a", "node-b"]
}
```

### Complete Download Flow

```bash
# 1. Lookup file locations
curl http://localhost:8080/api/lookup?fileId=abc123

# Response: [{"nodeId":"node-a","url":"http://localhost:9001"}]

# 2. Download from node (direct or via proxy)
curl "http://localhost:8080/api/download?fileId=abc123&nodeUrl=http://localhost:9001" \
  -o downloaded.pdf
```

---

## Authentication

Current version: **No authentication** (demo/development only)

For production, implement:
- API keys in headers: `X-API-Key: your-key`
- JWT tokens for user sessions
- OAuth 2.0 for third-party integrations

---

## Rate Limiting

Current version: **No rate limiting**

For production, implement:
- Per-IP rate limits
- Per-user quotas
- Storage quota per account

---

## Versioning

Current API version: **v1** (implicit)

Future versions should use URL prefix: `/api/v2/...`
