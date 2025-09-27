compile: go build sseproxy.go
run: ./sseproxy -upstream http://127.0.0.1:9292 -host 127.0.0.1 -port 6292
test: ./test.sh
