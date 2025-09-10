# Search Collector AI

**Vibe coding experiment.** This is an attempt to rewrite the stolostron/search-collector project using only AI prompts.

A production-ready Kubernetes resource collector that watches all cluster resources, transforms them for search indexing, and syncs changes to a search indexer service.

## Features

- **Memory-Optimized Informers**: Watches all Kubernetes resources without local caching to minimize memory usage
- **Dynamic Resource Discovery**: Automatically discovers and watches all available resource types in the cluster
- **Configurable Field Extraction**: Extract kubectl-equivalent fields plus custom fields via JSON configuration
- **Relationship Discovery**: Automatically discovers relationships between Kubernetes resources
- **Resilient Sync Processing**: Robust error handling with exponential backoff and retry logic
- **Comprehensive Monitoring**: Built-in status server with detailed metrics and health checks
- **Production Ready**: Graceful shutdown, proper logging, and battle-tested patterns

## Architecture

The collector consists of several key components:

### Informer (`pkg/informer`)
- Connects to Kubernetes API using client-go library
- Watches all Kubernetes resources with memory optimization
- Streams events without local resource caching to prevent OOM issues

### Transformer (`pkg/transformer`) 
- Receives events from informers and extracts searchable data
- Supports multiple transformer types: base, resource-specific, and configurable
- Extracts fields equivalent to `kubectl get` output plus custom fields
- Discovers relationships between resources (owner references, etc.)
- **UID Extraction**: Ensures every resource has a UID for search indexing

### Reconciler (`pkg/reconciler`)
- Maintains in-memory state of all transformed resources
- Merges changes and tracks resource lifecycle
- Provides change detection and cleanup of deleted resources
- Memory-efficient with configurable cleanup intervals

### Sender (`pkg/sender`)
- Syncs state changes to the search indexer via HTTP
- Batches resources for efficient transmission
- Implements retry logic with exponential backoff
- Converts internal state to search indexer payload format

## Quick Start

### Prerequisites
- Go 1.19+
- Access to a Kubernetes cluster
- Search indexer service running (default: `https://localhost:3010/sync`)

### Installation

```bash
# Clone the repository
git clone https://github.com/jlpadilla/search-collector-ai.git
cd search-collector-ai

# Install dependencies
go mod tidy

# Build the application
go build -o search-collector-ai

# Run with default configuration
./search-collector-ai
```

### Configuration

The collector uses configuration files and environment variables:

```bash
# Basic configuration via environment
export INDEXER_URL="https://your-search-indexer:3010/sync"
export INDEXER_API_KEY="your-api-key"

# TLS configuration (optional)
export TLS_INSECURE_SKIP_VERIFY="false"  # Set to true only for testing
export TLS_CA_CERT_FILE="/path/to/ca.crt"
export TLS_CLIENT_CERT_FILE="/path/to/client.crt"
export TLS_CLIENT_KEY_FILE="/path/to/client.key"

# Run with custom transformer config
./search-collector-ai --transformer-config=./configs/transform.json
```

## Configuration

### Transform Configuration (`configs/transform.json`)

The transformer supports extensive field extraction configuration:

```json
{
  "global": {
    "defaultFields": [
      "metadata.uid",
      "metadata.name", 
      "metadata.namespace",
      "metadata.creationTimestamp",
      "metadata.labels"
    ],
    "includeLabels": true,
    "includeAnnotations": false,
    "discoverRelationships": true
  },
  "resources": {
    "pods": {
      "additionalFields": [
        "spec.nodeName",
        "status.phase",
        "status.podIP"
      ],
      "customFields": [
        {
          "fieldName": "container_count",
          "jsonPath": "spec.containers.length",
          "type": "int"
        }
      ]
    }
  }
}
```

### TLS/HTTPS Configuration

The collector uses HTTPS by default for secure communication with the search indexer. You can configure TLS settings:

#### Production Setup (Recommended)
```bash
export INDEXER_URL="https://your-search-indexer.company.com:3010/sync"
export TLS_CA_CERT_FILE="/etc/ssl/certs/ca.crt"          # Custom CA certificate
export TLS_CLIENT_CERT_FILE="/etc/ssl/certs/client.crt"  # Client certificate for mutual TLS
export TLS_CLIENT_KEY_FILE="/etc/ssl/private/client.key" # Client private key
export TLS_SERVER_NAME="search-indexer.company.com"     # Override server name for verification
```

#### Development/Testing Setup (Less Secure)
```bash
export INDEXER_URL="https://localhost:3010/sync"
export TLS_INSECURE_SKIP_VERIFY="true"  # Skip certificate verification (NOT for production)
```

#### Security Notes
- **Default**: TLS certificate verification is **enabled** for security
- **Production**: Always use valid certificates and avoid `TLS_INSECURE_SKIP_VERIFY=true`
- **Mutual TLS**: Configure client certificates if your indexer requires client authentication
- **Custom CAs**: Use `TLS_CA_CERT_FILE` for private/internal certificate authorities

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `INDEXER_URL` | Search indexer endpoint | `https://localhost:3010/sync` |
| `INDEXER_API_KEY` | API key for indexer authentication | `` |
| `TLS_INSECURE_SKIP_VERIFY` | Skip TLS certificate verification (insecure) | `false` |
| `TLS_CA_CERT_FILE` | Path to custom CA certificate file | `` |
| `TLS_CLIENT_CERT_FILE` | Path to client certificate file | `` |
| `TLS_CLIENT_KEY_FILE` | Path to client private key file | `` |
| `TLS_SERVER_NAME` | Server name for TLS verification | `` |
| `SEND_INTERVAL` | How often to sync changes | `10s` |
| `BATCH_SIZE` | Maximum resources per batch | `100` |
| `MAX_RETRIES` | Maximum retry attempts | `3` |

## API Documentation

### Status Endpoints

The built-in status server (default `:8080`) provides monitoring endpoints:

- `GET /health` - Health check
- `GET /status` - Overall system status  
- `GET /reconciler/stats` - Detailed reconciler statistics
- `GET /reconciler/resources` - List managed resources
- `GET /reconciler/resources?type=pods` - Filter by resource type
- `GET /reconciler/resources?changed=true` - Only changed resources
- `GET /sender/stats` - Sender performance metrics

### Search Indexer Payload

The collector sends data to the search indexer using this format:

```json
{
  "addResources": [
    {
      "kind": "Pod",
      "uid": "pod-uid-12345",
      "resourceString": "default/my-pod",
      "properties": {
        "name": "my-pod",
        "namespace": "default",
        "status_phase": "Running",
        "spec_nodeName": "worker-1",
        "label_app": "frontend"
      }
    }
  ],
  "updateResources": [...],
  "deleteResources": [...],
  "addEdges": [...],
  "deleteEdges": [...]
}
```

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run specific package tests
go test ./pkg/transformer -v
go test ./pkg/sender -v
```

### Building

```bash
# Build for current platform
go build

# Build for Linux
GOOS=linux GOARCH=amd64 go build

# Build with version info
go build -ldflags "-X main.version=v1.0.0"
```

### Troubleshooting

#### Diagnostics Mode

If you're experiencing issues, use the built-in diagnostics tool:

```bash
# Run comprehensive diagnostics
./search-collector-ai -diagnose

# This will test:
# - Configuration validation
# - Search indexer connectivity 
# - TLS/HTTPS setup
# - Actual sync event transmission
```

#### Common HTTP 404 Error

If you see `Failed to process changes: HTTP 404`, run diagnostics for specific guidance:

```bash
./search-collector-ai -diagnose
```

The diagnostics will provide specific troubleshooting steps based on the error type.

#### Debug Logging

Enable verbose logging for detailed troubleshooting:

```bash
# Detailed HTTP request/response logging
./search-collector-ai -v=4

# This shows:
# - TLS configuration details
# - HTTP request URLs and payloads
# - Detailed error responses
# - Connectivity test results
```

See [TROUBLESHOOTING.md](TROUBLESHOOTING.md) for comprehensive troubleshooting guide.

### Code Structure

```
├── main.go                    # Application entry point
├── pkg/
│   ├── config/               # Configuration management
│   ├── discovery/            # Kubernetes resource discovery
│   ├── informer/             # Memory-optimized Kubernetes informers  
│   ├── transformer/          # Resource transformation and field extraction
│   ├── reconciler/           # State management and change tracking
│   ├── sender/               # Search indexer synchronization
│   ├── handler/              # Event processing pipeline
│   └── status/               # Health and monitoring endpoints
├── configs/
│   └── transform.json        # Field extraction configuration
└── tests/                    # Integration tests
```

## Recent Improvements

### HTTPS/TLS Support ✅
- **Enhancement**: Upgraded from HTTP to HTTPS for secure communication with search indexer
- **Features**:
  - HTTPS by default with proper TLS certificate verification
  - Support for custom CA certificates for private certificate authorities
  - Mutual TLS authentication with client certificates
  - Configurable TLS settings via environment variables
  - Production-ready security with option to disable verification for development
- **Security**: TLS certificate verification enabled by default, with clear warnings for insecure configurations

### UID Extraction Fix ✅
- **Issue**: Resources without UIDs were causing converter failures
- **Solution**: Enhanced transformer to guarantee UID extraction from all resources
- **Implementation**: 
  - Base transformer now explicitly extracts UID from ObjectMeta
  - Converter has fallback UID generation for edge cases
  - Comprehensive test coverage for UID handling
- **Result**: 100% reliable resource processing across all Kubernetes resource types

### Memory Optimization
- Informers stream events without local caching
- Configurable reconciler cleanup to prevent memory leaks
- Efficient field extraction to minimize memory footprint

### Production Readiness
- Graceful shutdown handling
- Comprehensive error handling and logging
- Health checks and monitoring endpoints
- Retry logic with exponential backoff

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Run `go test ./...` and `go fmt ./...`
5. Submit a pull request

## License

[Add your license here]

## Acknowledgments

This project is inspired by the [stolostron/search-collector](https://github.com/stolostron/search-collector) project and implements similar functionality with modern Go patterns and AI-assisted development.