# Troubleshooting Guide

## Common Issues and Solutions

### HTTP 404 Error: "Search indexer endpoint not found"

**Error message:**
```
Failed to process changes: failed to send sync event 1: failed to send sync event after 4 attempts: HTTP 404: 
```

**Possible causes and solutions:**

#### 1. Search Indexer Service Not Running
**Check if the search indexer service is running:**
```bash
# Check if service is listening on port 3010
netstat -ln | grep 3010
# or
lsof -i :3010

# If using Docker
docker ps | grep search-indexer

# If using Kubernetes
kubectl get pods -l app=search-indexer
kubectl get svc search-indexer
```

#### 2. Incorrect URL Configuration
**Common URL issues:**
- **Wrong port**: Default is `3010`, check your indexer service configuration
- **Wrong path**: The endpoint should end with `/sync`
- **Wrong protocol**: Make sure to use `https://` (or `http://` if your indexer doesn't support HTTPS)

**Test the URL manually:**
```bash
# Test connectivity
curl -k -X POST https://localhost:3010/sync \
  -H "Content-Type: application/json" \
  -d '{"addResources":[]}'

# If you get 404, try different variations:
curl -k -X POST https://localhost:3010/api/sync -H "Content-Type: application/json" -d '{}'
curl -k -X POST https://localhost:3010/v1/sync -H "Content-Type: application/json" -d '{}'
curl -k -X POST http://localhost:3010/sync -H "Content-Type: application/json" -d '{}'
```

#### 3. Network Connectivity Issues
**Test basic connectivity:**
```bash
# Test if port is reachable
telnet localhost 3010

# Test HTTPS specifically
openssl s_client -connect localhost:3010

# Test HTTP if HTTPS fails
curl -X GET http://localhost:3010/health
```

#### 4. TLS/Certificate Issues
**If using HTTPS and getting connection errors:**
```bash
# Test with insecure TLS (for debugging only)
export TLS_INSECURE_SKIP_VERIFY=true

# Check certificate
openssl s_client -connect your-indexer:3010 -servername your-indexer
```

### Configuration Fixes

#### Fix 1: Update Indexer URL
```bash
# Set the correct URL
export INDEXER_URL="https://your-search-indexer:3010/sync"

# Or if your indexer uses HTTP
export INDEXER_URL="http://your-search-indexer:3010/sync"
```

#### Fix 2: Disable TLS Verification (Development Only)
```bash
export TLS_INSECURE_SKIP_VERIFY=true
```

#### Fix 3: Configure Custom CA Certificate
```bash
export TLS_CA_CERT_FILE="/path/to/your/ca.crt"
```

### Search Indexer Setup

If you don't have a search indexer running, here's a simple test server you can use for development:

#### Option 1: Simple HTTP Server for Testing
```python
#!/usr/bin/env python3
import http.server
import json
from http.server import BaseHTTPRequestHandler

class SyncHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path == '/sync':
            content_length = int(self.headers['Content-Length'])
            post_data = self.rfile.read(content_length)
            
            try:
                data = json.loads(post_data.decode('utf-8'))
                print(f"Received sync event: {json.dumps(data, indent=2)}")
                
                self.send_response(200)
                self.send_header('Content-type', 'application/json')
                self.end_headers()
                self.wfile.write(b'{"status": "success"}')
            except Exception as e:
                print(f"Error processing request: {e}")
                self.send_response(400)
                self.end_headers()
        else:
            self.send_response(404)
            self.end_headers()

if __name__ == '__main__':
    server = http.server.HTTPServer(('localhost', 3010), SyncHandler)
    print("Test search indexer running on http://localhost:3010")
    print("Sync endpoint: http://localhost:3010/sync")
    server.serve_forever()
```

Save as `test-indexer.py` and run:
```bash
python3 test-indexer.py
```

Then configure the collector to use HTTP:
```bash
export INDEXER_URL="http://localhost:3010/sync"
export TLS_INSECURE_SKIP_VERIFY=true
```

#### Option 2: Using curl to simulate an indexer
```bash
# In one terminal, create a simple listener
nc -l 3010

# In another terminal, test the collector
# The collector should connect and you'll see the POST request
```

### Debug Logging

Enable verbose logging to see more details about the HTTP requests:

```bash
# Run with debug logging
./search-collector-ai -v=4

# This will show:
# - TLS configuration details
# - HTTP request URLs and payload sizes
# - Detailed error responses
# - Connectivity test results
```

### Verification Steps

1. **Check indexer service status**
2. **Verify URL configuration** 
3. **Test connectivity manually with curl**
4. **Check network/firewall rules**
5. **Verify TLS/certificate configuration**
6. **Enable debug logging**
7. **Use test indexer if needed**

### Getting Help

If you're still having issues:

1. **Check the logs** with `-v=4` for detailed output
2. **Test with a simple HTTP indexer** to isolate the problem
3. **Verify your search indexer documentation** for the correct endpoint URL
4. **Check firewall and network configuration**

The collector will provide detailed error messages including specific suggestions for 404 errors.