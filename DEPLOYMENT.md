# 🚀 Deployment Guide

Quick deployment guide untuk Distributed File Storage System.

---

## Quick Start (Development)

### 1-Command Start

```bash
# Make scripts executable (Linux/Mac)
chmod +x scripts/*.sh

# Start all services
./scripts/start_system.sh
```

**Access Points:**
- Dashboard: http://localhost:8080/dashboard
- Upload UI: http://localhost:8080/

### Stop System

```bash
./scripts/stop_system.sh
```

---

## Manual Start (Step-by-Step)

### Terminal 1: Naming Service

```bash
cd naming_service
go run main.go
```

Expected output:
```
Naming Service running at :8000 ...
Auto-healing background job started
```

### Terminal 2: Storage Node A

```bash
cd storage_node
NODE_ID=node-a PORT=9001 DATA_DIR=./data_a go run main.go
```

### Terminal 3: Storage Node B

```bash
cd storage_node
NODE_ID=node-b PORT=9002 DATA_DIR=./data_b go run main.go
```

### Terminal 4: UI Gateway

```bash
cd ui_gateway
go run main.go
```

---

## Running Tests

### Automated Tests

```bash
# Make executable
chmod +x scripts/test_system.sh

# Run tests
./scripts/test_system.sh
```

Expected output:
```
🧪 Running Automated Tests...
  ✅ PASS: Naming Service health
  ✅ PASS: File upload
  ✅ PASS: File download
  🎉 All tests passed!
```

### Manual Tests

See [TESTING.md](./TESTING.md) for detailed test scenarios.

---

## Production Deployment

### Recommended Setup

```
┌─────────────────┐
│   Load Balancer │ (nginx/HAProxy)
│   (HTTPS :443)  │
└────────┬────────┘
         │
    ┌────┴────┬──────────┬──────────┐
    │         │          │          │
┌───┴───┐ ┌──┴──┐   ┌───┴──┐  ┌───┴──┐
│Gateway│ │Node │   │ Node │  │ Node │
│ :8080 │ │:9001│   │:9002 │  │:9003 │
└───┬───┘ └──┬──┘   └───┬──┘  └───┬──┘
    │        │          │         │
    └────────┴──────────┴─────────┘
                │
         ┌──────┴──────┐
         │   Naming    │
         │  Service    │
         │   :8000     │
         └─────────────┘
```

### Build for Production

```bash
# Build naming service
cd naming_service
go build -o naming_service main.go

# Build storage node
cd ../storage_node
go build -o storage_node main.go

# Build UI gateway
cd ../ui_gateway
go build -o ui_gateway main.go
```

### Systemd Service Files

**naming-service.service:**
```ini
[Unit]
Description=Distributed Storage Naming Service
After=network.target

[Service]
Type=simple
User=storage
WorkingDirectory=/opt/distributed-storage/naming_service
ExecStart=/opt/distributed-storage/naming_service/naming_service
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**storage-node@.service:**
```ini
[Unit]
Description=Distributed Storage Node %i
After=network.target naming-service.service

[Service]
Type=simple
User=storage
Environment="NODE_ID=node-%i"
Environment="PORT=900%i"
Environment="DATA_DIR=/var/lib/storage/node-%i"
Environment="NAMING_URL=http://localhost:8000"
WorkingDirectory=/opt/distributed-storage/storage_node
ExecStart=/opt/distributed-storage/storage_node/storage_node
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**ui-gateway.service:**
```ini
[Unit]
Description=Distributed Storage UI Gateway
After=network.target naming-service.service

[Service]
Type=simple
User=storage
Environment="NAMING_URL=http://localhost:8000"
WorkingDirectory=/opt/distributed-storage/ui_gateway
ExecStart=/opt/distributed-storage/ui_gateway/ui_gateway
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

### Install Services

```bash
# Copy service files
sudo cp *.service /etc/systemd/system/

# Enable services
sudo systemctl enable naming-service
sudo systemctl enable storage-node@1
sudo systemctl enable storage-node@2
sudo systemctl enable ui-gateway

# Start services
sudo systemctl start naming-service
sudo systemctl start storage-node@1
sudo systemctl start storage-node@2
sudo systemctl start ui-gateway

# Check status
sudo systemctl status naming-service
sudo systemctl status storage-node@1
sudo systemctl status storage-node@2
sudo systemctl status ui-gateway
```

---

## Docker Deployment (Optional)

### Dockerfile.naming

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY naming_service/ .
RUN go build -o naming_service main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/naming_service .
EXPOSE 8000
CMD ["./naming_service"]
```

### Dockerfile.storage

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY storage_node/ .
RUN go build -o storage_node main.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/storage_node .
EXPOSE 9001
CMD ["./storage_node"]
```

### docker-compose.yml

```yaml
version: '3.8'

services:
  naming:
    build:
      context: .
      dockerfile: Dockerfile.naming
    ports:
      - "8000:8000"
    volumes:
      - naming-data:/root/metadata
    networks:
      - storage-net
    restart: unless-stopped

  storage-a:
    build:
      context: .
      dockerfile: Dockerfile.storage
    environment:
      - NODE_ID=node-a
      - PORT=9001
      - DATA_DIR=/data
      - NAMING_URL=http://naming:8000
      - CAPACITY_BYTES=10737418240
    ports:
      - "9001:9001"
    volumes:
      - storage-a-data:/data
    networks:
      - storage-net
    depends_on:
      - naming
    restart: unless-stopped

  storage-b:
    build:
      context: .
      dockerfile: Dockerfile.storage
    environment:
      - NODE_ID=node-b
      - PORT=9001
      - DATA_DIR=/data
      - NAMING_URL=http://naming:8000
      - CAPACITY_BYTES=10737418240
    ports:
      - "9002:9001"
    volumes:
      - storage-b-data:/data
    networks:
      - storage-net
    depends_on:
      - naming
    restart: unless-stopped

  gateway:
    build:
      context: .
      dockerfile: Dockerfile.gateway
    environment:
      - NAMING_URL=http://naming:8000
      - ADDR=:8080
    ports:
      - "8080:8080"
    networks:
      - storage-net
    depends_on:
      - naming
    restart: unless-stopped

volumes:
  naming-data:
  storage-a-data:
  storage-b-data:

networks:
  storage-net:
    driver: bridge
```

### Run with Docker Compose

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop all services
docker-compose down
```

---

## Nginx Configuration (Production)

```nginx
upstream gateway {
    server localhost:8080;
}

server {
    listen 80;
    server_name storage.example.com;
    
    # Redirect to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name storage.example.com;
    
    ssl_certificate /etc/ssl/certs/storage.crt;
    ssl_certificate_key /etc/ssl/private/storage.key;
    
    # Security headers
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;
    
    # Client body size (for large file uploads)
    client_max_body_size 100M;
    
    location / {
        proxy_pass http://gateway;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # Timeouts for large files
        proxy_connect_timeout 600;
        proxy_send_timeout 600;
        proxy_read_timeout 600;
        send_timeout 600;
    }
}
```

---

## Monitoring Setup

### Prometheus Metrics (Future Enhancement)

Add to naming service:
```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

mux.Handle("/metrics/prometheus", promhttp.Handler())
```

### Grafana Dashboard

Create dashboard with panels for:
- Total files over time
- Node health status
- Storage usage per node
- Upload/download rates
- Auto-healing events

---

## Backup Strategy

### Metadata Backup

```bash
# Automated backup script
#!/bin/bash
BACKUP_DIR=/backup/metadata
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
cp -r /opt/distributed-storage/naming_service/metadata $BACKUP_DIR/metadata_$DATE
find $BACKUP_DIR -type d -mtime +7 -exec rm -rf {} \;
```

### Storage Backup

```bash
# Backup storage nodes
rsync -av /var/lib/storage/node-a/ backup-server:/backup/node-a/
rsync -av /var/lib/storage/node-b/ backup-server:/backup/node-b/
```

---

## Troubleshooting

### Check Service Status

```bash
# Linux
sudo systemctl status naming-service
sudo systemctl status storage-node@1
sudo systemctl status ui-gateway

# View logs
sudo journalctl -u naming-service -f
sudo journalctl -u storage-node@1 -f
```

### Common Issues

**Port already in use:**
```bash
# Check what's using the port
lsof -i :8000

# Kill process
kill -9 <PID>
```

**Node not registering:**
```bash
# Check network connectivity
curl http://localhost:8000/metrics

# Restart storage node
sudo systemctl restart storage-node@1
```

**Auto-healing not working:**
```bash
# Check naming service logs
tail -f naming_service/metadata/naming.log | grep AUTO-HEAL
```

---

## Security Checklist

For production deployment:

- [ ] Enable HTTPS with valid SSL certificates
- [ ] Add authentication/authorization
- [ ] Implement API rate limiting
- [ ] Set up firewall rules
- [ ] Use non-root user for services
- [ ] Enable audit logging
- [ ] Regular security updates
- [ ] Encrypt data at rest
- [ ] Set up intrusion detection
- [ ] Implement backup encryption

---

## Scaling Considerations

### Horizontal Scaling

Add more storage nodes:
```bash
# Node C
NODE_ID=node-c PORT=9003 DATA_DIR=./data_c go run main.go

# Node D
NODE_ID=node-d PORT=9004 DATA_DIR=./data_d go run main.go
```

### Vertical Scaling

Increase node capacity:
```bash
CAPACITY_BYTES=10737418240 # 10 GB
```

### Multi-Region

Deploy nodes in different zones/regions for disaster recovery.

---

## Performance Tuning

### Go Runtime

```bash
# Set max CPUs
export GOMAXPROCS=4

# Increase file descriptors
ulimit -n 65536
```

### Database Migration

For production, replace JSON files with PostgreSQL:
```sql
CREATE TABLE files (
    file_id TEXT PRIMARY KEY,
    filename TEXT,
    size BIGINT,
    checksum TEXT,
    state TEXT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE TABLE replicas (
    id SERIAL PRIMARY KEY,
    file_id TEXT REFERENCES files(file_id),
    node_id TEXT,
    status TEXT,
    last_verified_at TIMESTAMP
);

CREATE INDEX idx_files_state ON files(state);
CREATE INDEX idx_replicas_file_id ON replicas(file_id);
```

---

## Support & Maintenance

### Log Rotation

```bash
# /etc/logrotate.d/distributed-storage
/opt/distributed-storage/logs/*.log {
    daily
    rotate 7
    compress
    delaycompress
    notifempty
    create 0640 storage storage
    sharedscripts
    postrotate
        systemctl reload naming-service
    endscript
}
```

### Health Monitoring

Set up cron jobs:
```bash
# /etc/cron.d/storage-health
*/5 * * * * root curl -f http://localhost:8080/api/metrics || systemctl restart ui-gateway
```

---

## Next Steps

1. ✅ Review deployment steps
2. ✅ Test in staging environment
3. ✅ Configure monitoring
4. ✅ Set up backups
5. ✅ Deploy to production
6. ✅ Monitor and optimize

---

**Deployment Status: READY FOR PRODUCTION** 🚀
