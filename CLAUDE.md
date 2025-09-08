# Search Collector AI

## Project Overview
Collects data from all resources in the kubernetes cluster and sends it to the search-indexer for indexing in the database.

## Tech Stack
- Go (primary language)
- Red Hat Advanced Cluster Management addon.

## Development Setup
```bash
# Install dependencies
go mod tidy

# Run the application
go run main.go

# Run tests
go test ./...

# Build
go build -o search-collector-ai
```

## Project Structure
```
├── main.go              # Entry point
├── internal/            # Private application code
├── pkg/                 # Public library code
├── cmd/                 # Command line applications
├── configs/             # Configuration files
├── docs/                # Documentation
└── tests/               # Test files
```

## Architecture
The overall search architecture is defined in this wiki page. https://github.com/stolostron/search-v2-operator/wiki/Feature-Spec
This project is for the search collector component.

The collector project contains the following components or packages.
### Informer
Connects to the Kubernetes API using the client-go library.
Watches all Kubernetes resources and sends the events to the transformer.
This is similar to the client-go informer, but optimized to reduce memory. It must not cache the entire resource locally.

### Transformer
Receives event data from the informer and extracts the data we want to send to the search index. 
Discovers relationships to other resources.

### Reconciler
Merges changes into the internal state.

### Sender
Syncs state and sends changes to the search-indexer.
Current communication uses an HTTPS request to POST state.
In the future, we should replace with gRPC or a message queue like Kafka.


## Key Commands
- **Build**: `go build`
- **Test**: `go test ./...`
- **Lint**: `golangci-lint run` (if using golangci-lint)
- **Format**: `go fmt ./...`

## Environment Variables
List any required environment variables here.

## Notes for Claude
- Follow Go conventions and idiomatic patterns
- Use standard library when possible
- Keep packages focused and well-documented
- Write tests for new functionality
- Optimize to reduce memory and cpu consumption.