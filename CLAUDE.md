# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

### Build and Run
```bash
# Build the binary
go build -o http-transit

# Run with default config
./http-transit

# Run with custom config
./http-transit -config custom.json
```

### Development
```bash
# Tidy dependencies
go mod tidy

# Test the service (requires config.json)
./http-transit &

# Test with curl
curl -H "Host: api.example.com" http://localhost:8080/test

# Stop the background service
pkill http-transit
# Or find and kill by port
lsof -ti:8080 | xargs kill
```

## Architecture Overview

HTTP Transit is a high-performance HTTP reverse proxy written in Go with domain-specific connection pooling.

### Core Components

1. **Configuration System** (`config.go`)
   - JSON-based configuration with server settings and transit rules
   - Supports multiple domain forwarding with different header policies
   - Server config includes port and public/private binding modes

2. **Proxy Handler** (`proxy.go`)
   - Implements HTTP handler with per-domain connection pooling
   - Connection pools are initialized at startup for all configured domains
   - No lazy loading - all pools created during initialization for better performance
   - Header processing with set/extra/remove/forward policies

3. **Main Application** (`main.go`)
   - Graceful shutdown with signal handling (SIGINT, SIGTERM)
   - Dynamic address binding based on public/private configuration
   - Simple flag parsing for config file path

4. **Logging System** (`log.go`)
   - Uses uber-go/zap structured logging
   - Configurable log levels (debug, info, warn, error, dpanic, panic, fatal)
   - Optional file output with dual output to stderr and file
   - Custom time and level formatting

### Key Architecture Decisions

**Connection Pooling Strategy:**
- Each backend domain gets its own HTTP client connection pool
- Pools are created at startup, not on-demand (no lazy loading)
- This eliminates runtime locking and provides predictable performance
- Each pool: 100 max idle connections, 20 per host, 100 max per host, 1-minute idle timeout

**Header Processing Pipeline:**
The order of header processing in `proxy.go:205` is critical:
1. Forward client headers if `forward_client` is true (excluding removed headers)
2. Add extra headers from `extra` map only if not already present
3. Set/override headers from `set` map (overwrites any existing headers)
4. Force set `Host` header to backend host
Note: Headers in the `remove` array are filtered during the forward step

**Configuration Structure:**
- `server`: port and public binding mode
- `transit_map`: mapping of frontend domains to backend configurations
- Each rule includes `backend_base`, `backend_prefix`, and `headers` configuration

### Performance Characteristics

- No runtime connection pool creation - all initialized at startup
- Per-domain connection isolation prevents cross-domain interference
- HTTP/2 and compression support enabled by default
- 30-second request timeout with 1-minute connection idle timeout

## Configuration Notes

- Port is configured via `config.json`, not command line
- `backend_base` auto-prepends `http://` if no protocol specified
- Headers are processed in order: forward (with remove filter) → extra → set → host
- Multiple domains pointing to the same backend host share the same connection pool (pools are keyed by host, not full URL)
- Log configuration supports `level` (debug/info/warn/error/dpanic/panic/fatal) and `file` (optional file path for dual output)
- Header removal is case-insensitive (config.go:70 converts to lowercase)

## Testing

The service can be tested by:
1. Running the server with a valid config.json
2. Making requests with appropriate Host headers
3. Verifying forwarding behavior and header transformations

Example test requests:
```bash
# Basic forwarding test
curl -H "Host: api.example.com" http://localhost:8080/some/path

# Test with custom headers
curl -H "Host: api.example.com" -H "Authorization: Bearer token" http://localhost:8080/api/users

# Test POST with body
curl -X POST -H "Host: api.example.com" -H "Content-Type: application/json" \
  -d '{"key":"value"}' http://localhost:8080/api/data

# Verbose output to see header transformations
curl -v -H "Host: api.example.com" http://localhost:8080/test
```

## Debugging

- Set log level to `debug` in config.json to see detailed request/response tracing
- Debug logs include full headers, body content (for text-based content), and timing information
- The `ProxyTrace` struct in proxy.go:16 captures comprehensive request flow data
- Binary content is logged with size and content-type metadata