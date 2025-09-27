package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	upstreamFlag  = flag.String("upstream", "", "Upstream origin base URL (e.g. https://api.openai.com)")
	hostFlag      = flag.String("host", "0.0.0.0", "Host/IP to listen on")
	portFlag      = flag.String("port", "8080", "Port to listen on")
	heartbeatFlag = flag.Duration("heartbeat", 10*time.Second, "SSE heartbeat interval (e.g. 10s)")
)

var hopByHop = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func stripHopByHop(h http.Header) {
	for k := range h {
		if _, ok := hopByHop[textproto.CanonicalMIMEHeaderKey(k)]; ok {
			h.Del(k)
		}
	}
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				h.Del(f)
			}
		}
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

type streamProbe struct {
	Stream *bool `json:"stream"`
}

type flushingWriter struct {
	w          http.ResponseWriter
	flusher    http.Flusher
	firstWrite *bool
}

func (fw *flushingWriter) Write(p []byte) (int, error) {
	if !*fw.firstWrite && len(p) > 0 {
		*fw.firstWrite = true
	}
	n, err := fw.w.Write(p)
	fw.flusher.Flush()
	return n, err
}

func main() {
	flag.Parse()
	if *upstreamFlag == "" {
		log.Println("error: -upstream is required, e.g. -upstream https://api.openai.com")
		os.Exit(2)
	}
	baseURL, err := url.Parse(*upstreamFlag)
	if err != nil {
		log.Fatalf("invalid upstream URL: %v", err)
	}

	// HTTP client for upstream. Disable redirects to avoid accidental replays.
	client := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read client body (we’ll reuse it for the upstream request).
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		// Detect streaming in a minimal way.
		streaming := false
		if len(bodyBytes) > 0 {
			var sp streamProbe
			if json.Unmarshal(bodyBytes, &sp) == nil && sp.Stream != nil && *sp.Stream {
				streaming = true
			}
		}
		// Heuristic: also treat as streaming if client asks for SSE.
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			streaming = true
		}

		// Build upstream URL
		up := *baseURL
		up.Path = strings.TrimRight(baseURL.Path, "/") + r.URL.Path
		up.RawQuery = r.URL.RawQuery

		log.Printf("upstream URL: %s, streaming: %t, body size: %d", up.String(), streaming, len(bodyBytes))

		// Prepare upstream request
		upReq, err := http.NewRequestWithContext(r.Context(), r.Method, up.String(), bytes.NewReader(bodyBytes))
		if err != nil {
			http.Error(w, "failed to create upstream request", http.StatusBadGateway)
			return
		}
		upReq.Header = r.Header.Clone()
		stripHopByHop(upReq.Header)

		if os.Getenv("SSEPROXY_DUMP") == "1" {
			if dump, err := httputil.DumpRequestOut(upReq, false); err == nil {
				log.Printf(">>> upstream request:\n%s", dump)
			}
		}

		if !streaming {
			// Non-streaming: simple pass-through.
			upResp, err := client.Do(upReq)
			if err != nil {
				http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
				return
			}
			defer upResp.Body.Close()

			copyHeaders(w.Header(), upResp.Header)
			stripHopByHop(w.Header())
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(upResp.StatusCode)
			_, _ = io.Copy(w, upResp.Body)
			return
		}

		// STREAMING: respond immediately (before upstream headers) to beat TTFB limits.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported (no flusher)", http.StatusInternalServerError)
			return
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		stripHopByHop(h)

		// We intentionally return 200 now; upstream errors will be sent as SSE "error" event.
		w.WriteHeader(http.StatusOK)

		firstUpstreamByte := false

		// Immediate first byte + start heartbeat ticker
		_, _ = w.Write([]byte(": ping\n\n"))
		flusher.Flush()

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		go func() {
			t := time.NewTicker(*heartbeatFlag)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if firstUpstreamByte {
						return
					}
					_, _ = w.Write([]byte(": ping\n\n"))
					flusher.Flush()
				}
			}
		}()

		// Dial upstream concurrently (with a connect header-timeout for quicker failures).
		upRespCh := make(chan *http.Response, 1)
		upErrCh := make(chan error, 1)

		go func() {
			// Use original request context for the upstream request
			// Don't use timeout context as it will cancel body reading
			req := upReq.WithContext(r.Context())
			log.Printf("making upstream request to %s", req.URL.String())
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("upstream request failed: %v", err)
				upErrCh <- err
				return
			}
			log.Printf("upstream request succeeded: status=%d", resp.StatusCode)
			upRespCh <- resp
		}()

		// Wait for client cancel, upstream error, or upstream response
		select {
		case <-r.Context().Done():
			return

		case err := <-upErrCh:
			// Surface upstream dial error via SSE event and end.
			payload, _ := json.Marshal(map[string]string{"error": err.Error()})
			w.Write([]byte("event: error\n"))
			w.Write([]byte("data: " + string(payload) + "\n\n"))
			flusher.Flush()
			return

		case upResp := <-upRespCh:
			defer upResp.Body.Close()

			log.Printf("upstream response: status=%d, content-type=%s", upResp.StatusCode, upResp.Header.Get("Content-Type"))

			// If upstream non-2xx before any data, emit an error event and exit.
			if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
				errBody, _ := io.ReadAll(io.LimitReader(upResp.Body, 4096))
				payload, _ := json.Marshal(map[string]any{
					"status": upResp.StatusCode,
					"body":   string(errBody),
				})
				w.Write([]byte("event: error\n"))
				w.Write([]byte("data: " + string(payload) + "\n\n"))
				flusher.Flush()
				return
			}

			// Pipe upstream body → client; mark first upstream byte on first write.
			fw := &flushingWriter{w: w, flusher: flusher, firstWrite: &firstUpstreamByte}
			bytesWritten, copyErr := io.Copy(fw, upResp.Body)
			log.Printf("copied %d bytes from upstream, error: %v", bytesWritten, copyErr)

			// Graceful end marker
			w.Write([]byte(": done\n\n"))
			flusher.Flush()

			if copyErr != nil && ctx.Err() == nil {
				log.Printf("stream copy error: %v", copyErr)
			}
			return
		}
	})

	addr := *hostFlag + ":" + *portFlag
	srv := &http.Server{
		Addr:              addr,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 15 * time.Second,
		// No WriteTimeout to allow long-running streams
	}

	// Graceful shutdown
	go func() {
		log.Printf("SSE keepalive proxy listening on http://%s -> upstream %s (heartbeat=%s)", addr, baseURL.String(), heartbeatFlag.String())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("server shut down")
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s from %s in %v", r.Method, r.URL.Path, r.URL.RawQuery, r.RemoteAddr, time.Since(start))
	})
}
