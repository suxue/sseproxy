# Development Notes

## Building and Testing

### Build
```bash
go build sseproxy.go
```

### Run with Debug Logging
```bash
SSEPROXY_DUMP=1 ./sseproxy -upstream http://127.0.0.1:9292 -host 127.0.0.1 -port 6292
```

### Test SSE Streaming
```bash
curl -v -H 'accept: */*' -H 'content-type: application/json' \
  -X POST http://127.0.0.1:6292/v1/chat/completions \
  -d '{"model":"test","stream":true,"messages":[{"role":"user","content":"hello"}]}'
```

### Test Non-Streaming
```bash
curl -v -H 'content-type: application/json' \
  -X POST http://127.0.0.1:6292/v1/chat/completions \
  -d '{"model":"test","stream":false,"messages":[{"role":"user","content":"hello"}]}'
```

## Debugging

- Set `SSEPROXY_DUMP=1` to see upstream request dumps
- Check server logs for upstream response status and bytes copied
- Verify streaming detection by checking log output for "streaming: true/false"
- Monitor for "context canceled" errors which indicate premature connection closure

## Key Behaviors

- SSE detection: `"stream": true` in JSON body OR `Accept: text/event-stream` header
- Immediate SSE headers sent to client before upstream connection
- Heartbeat pings (`": ping\n\n"`) sent while waiting for upstream
- Complete upstream response streamed to client
- Graceful end marker (`": done\n\n"`) after upstream closes