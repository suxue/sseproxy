# SSE Proxy

A simple HTTP proxy for Server-Sent Events (SSE) streaming endpoints with keepalive functionality.

## Usage

```bash
go build sseproxy.go
./sseproxy -upstream <upstream_url> -host <host> -port <port>
```

### Options

- `-upstream`: Upstream server URL (required, e.g., `https://api.openai.com`)
- `-host`: Host/IP to listen on (default: `0.0.0.0`)
- `-port`: Port to listen on (default: `8080`)
- `-heartbeat`: SSE heartbeat interval (default: `10s`)

### Example

```bash
./sseproxy -upstream http://127.0.0.1:9292 -host 127.0.0.1 -port 6292
```

## Features

- Automatic SSE detection via `stream: true` in JSON body or `Accept: text/event-stream` header
- Immediate response headers to beat TTFB limits
- Heartbeat pings during upstream connection setup
- Proper streaming proxy that forwards complete upstream responses
- Graceful shutdown support

## Environment Variables

- `SSEPROXY_DUMP=1`: Enable request dumping for debugging
