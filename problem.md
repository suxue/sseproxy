```
$ ./sseproxy -upstream http://127.0.0.1:9292 -host 127.0.0.1 -port 6292
2025/09/27 14:53:40 SSE keepalive proxy listening on http://127.0.0.1:6292 -> upstream http://127.0.0.1:9292 (heartbeat=10s)
2025/09/27 14:53:50 stream copy error: context canceled
2025/09/27 14:53:50 POST /v1/chat/completions  from 127.0.0.1:58532 in 1.050935792s
^C2025/09/27 14:54:09 server shut down
```

debug the go code why proxy failed
