# SSE Proxy

A simple HTTP proxy for Server-Sent Events (SSE) streaming endpoints with keepalive functionality.

## Usage

```bash
go build sseproxy.go
./sseproxy -host <host> -port <port>
```

### Options

- `-host`: Host/IP to listen on (default: `0.0.0.0`)
- `-port`: Port to listen on (default: `8080`)
- `-heartbeat`: SSE heartbeat interval (default: `10s`)

### Example

```bash
# Start the proxy
./sseproxy -host 127.0.0.1 -port 6292

# Make a request with the upstream host specified in the X-Upstream-Host header
curl -v -H 'content-type: application/json' \
  -H 'X-Upstream-Host: localhost:8523' \
  -X POST http://127.0.0.1:6292/v1/chat/completions \
  -d '{"model":"test","stream":false,"messages":[{"role":"user","content":"hello"}]}'
```

### X-Upstream-Host Header

The proxy requires the `X-Upstream-Host` header to specify the target upstream server for each request. The header value can be in one of these formats:

- **Full URL with scheme**: `http://localhost:8523` or `https://api.openai.com`
- **Host and port**: `localhost:8523` or `192.168.1.100:3000` (defaults to `http://`)
- **Hostname only**: `api.openai.com` (defaults to `http://`, port 80)

Examples:
```bash
# Local development server
-H 'X-Upstream-Host: localhost:8523'

# Remote API with HTTPS
-H 'X-Upstream-Host: https://api.openai.com'

# IP address with custom port
-H 'X-Upstream-Host: 192.168.1.100:3000'
```

## Features

- Automatic SSE detection via `stream: true` in JSON body or `Accept: text/event-stream` header
- Immediate response headers to beat TTFB limits
- Heartbeat pings during upstream connection setup
- Proper streaming proxy that forwards complete upstream responses
- Graceful shutdown support

## Environment Variables

- `SSEPROXY_DUMP=1`: Enable request dumping for debugging
