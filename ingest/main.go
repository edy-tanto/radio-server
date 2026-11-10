// Radio ingest: browser mic (WebSocket, webm/opus) -> per-channel always-on ffmpeg
// -> Icecast (MP3, /<ch>.mp3) + HLS (AAC, /hls/<ch>/index.m3u8, CDN origin).
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	rate        = 44100             // mono s16le between decoder and encoder
	bytesPerSec = rate * 2
	primeBytes  = bytesPerSec * 3 / 10 // 300 ms jitter buffer before playing live audio
	maxQueue    = bytesPerSec           // drop oldest beyond 1 s (clock drift guard)
)

//go:embed public/*.html
var pages embed.FS

var (
	icecastURL       = env("ICECAST_URL", "icecast://source:changeme-source@localhost:8000")
	icecastPublicURL = env("ICECAST_PUBLIC_URL", "http://localhost:8000")
	hlsPublicURL     = env("HLS_PUBLIC_URL", "") // CDN base URL in production
	hlsDir           = env("HLS_DIR", "/hls")
	token            = env("BROADCAST_TOKEN", "")
)

type channel struct {
	name    string
	mu      sync.Mutex
	queue   []byte
	priming bool
	owner   *websocket.Conn // current broadcaster, nil when off air
}

func (c *channel) push(b []byte, from *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.owner != from {
		return
	}
	c.queue = append(c.queue, b...)
	if len(c.queue) > maxQueue {
		c.queue = c.queue[len(c.queue)-maxQueue:]
	}
}

// take n bytes of live audio, padded with silence (zeros)
func (c *channel) take(n int) []byte {
	out := make([]byte, n)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.priming {
		if len(c.queue) < primeBytes {
			return out
		}
		c.priming = false
	}
	k := copy(out, c.queue)
	c.queue = c.queue[k:]
	if k < n {
		c.priming = true // underrun: rebuild the jitter buffer
	}
	return out
}

func (c *channel) live() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.owner != nil
}

// runEncoder keeps one ffmpeg alive per channel, fed in real time with live audio or silence.
func (c *channel) runEncoder() {
	for {
		dir := filepath.Join(hlsDir, c.name)
		os.RemoveAll(dir)
		os.MkdirAll(dir, 0o755)
		cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning",
			"-f", "s16le", "-ar", fmt.Sprint(rate), "-ac", "1", "-i", "pipe:0",
			// output 1: Icecast MP3
			"-map", "0:a", "-c:a", "libmp3lame", "-b:a", "64k",
			"-content_type", "audio/mpeg", "-ice_name", c.name, "-f", "mp3", icecastURL+"/"+c.name+".mp3",
			// output 2: HLS AAC
			"-map", "0:a", "-c:a", "aac", "-b:a", "64k",
			"-f", "hls", "-hls_time", "4", "-hls_list_size", "6",
			"-hls_flags", "delete_segments+independent_segments",
			"-hls_segment_filename", filepath.Join(dir, "seg_%05d.ts"), filepath.Join(dir, "index.m3u8"),
		)
		cmd.Stderr = os.Stderr
		stdin, _ := cmd.StdinPipe()
		if err := cmd.Start(); err != nil {
			log.Printf("[%s] encoder start: %v", c.name, err)
		} else {
			log.Printf("[%s] encoder started", c.name)
			start, sent := time.Now(), int64(0)
			tick := time.NewTicker(20 * time.Millisecond)
			for range tick.C {
				due := int64(time.Since(start))*rate/int64(time.Second) - sent
				if due <= 0 {
					continue
				}
				if _, err := stdin.Write(c.take(int(due) * 2)); err != nil {
					break
				}
				sent += due
			}
			tick.Stop()
			stdin.Close()
			err := cmd.Wait()
			log.Printf("[%s] encoder exited (%v), restarting in 2s", c.name, err)
		}
		time.Sleep(2 * time.Second)
	}
}

func main() {
	channels := map[string]*channel{}
	var order []string
	for _, n := range strings.Split(env("CHANNELS", "main"), ",") {
		if n = strings.TrimSpace(n); n != "" {
			channels[n] = &channel{name: n, priming: true}
			order = append(order, n)
			go channels[n].runEncoder()
		}
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/channels", func(w http.ResponseWriter, r *http.Request) {
		type info struct {
			Name string `json:"name"`
			Live bool   `json:"live"`
			MP3  string `json:"mp3"`
			HLS  string `json:"hls"`
		}
		list := []info{}
		for _, n := range order {
			list = append(list, info{n, channels[n].live(),
				icecastPublicURL + "/" + n + ".mp3", hlsPublicURL + "/hls/" + n + "/index.m3u8"})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(list)
	})

	// HLS origin (put the CDN in front of this)
	hlsFiles := http.StripPrefix("/hls/", http.FileServer(http.Dir(hlsDir)))
	mux.HandleFunc("GET /hls/", func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		if strings.HasSuffix(r.URL.Path, ".m3u8") {
			h.Set("Content-Type", "application/vnd.apple.mpegurl")
			h.Set("Cache-Control", "public, max-age=2")
		} else {
			h.Set("Content-Type", "video/mp2t")
			h.Set("Cache-Control", "public, max-age=3600, immutable")
		}
		hlsFiles.ServeHTTP(w, r)
	})

	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := pages.ReadFile("public/" + name)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /{$}", page("index.html"))
	mux.HandleFunc("GET /broadcast", page("broadcast.html"))

	// WebSocket ingest: one broadcaster per channel
	mux.HandleFunc("GET /ingest", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil) // same-origin only by default
		if err != nil {
			return
		}
		conn.SetReadLimit(1 << 20)
		q := r.URL.Query()
		c := channels[q.Get("ch")]
		switch {
		case c == nil:
			conn.Close(4004, "unknown channel")
			return
		case token != "" && q.Get("token") != token:
			conn.Close(4001, "bad token")
			return
		}
		c.mu.Lock()
		busy := c.owner != nil
		if !busy {
			c.owner = conn
		}
		c.mu.Unlock()
		if busy {
			conn.Close(4009, "channel already live")
			return
		}
		ingest(r.Context(), c, conn)
	})

	port := env("PORT", "8080")
	log.Printf("ingest on :%s, channels: %v", port, order)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// ingest decodes whatever the browser recorded (webm/opus) into PCM for the channel queue.
func ingest(ctx context.Context, c *channel, conn *websocket.Conn) {
	log.Printf("[%s] broadcaster connected", c.name)
	defer func() {
		c.mu.Lock()
		c.owner = nil
		c.mu.Unlock()
		log.Printf("[%s] broadcaster disconnected", c.name)
	}()

	dec := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-fflags", "nobuffer", "-probesize", "32768", "-i", "pipe:0",
		"-f", "s16le", "-ar", fmt.Sprint(rate), "-ac", "1", "pipe:1")
	dec.Stderr = os.Stderr
	stdin, _ := dec.StdinPipe()
	stdout, _ := dec.StdoutPipe()
	if err := dec.Start(); err != nil {
		conn.Close(websocket.StatusInternalError, "decoder failed")
		return
	}
	defer func() {
		stdin.Close()
		time.AfterFunc(3*time.Second, func() { dec.Process.Kill() })
		dec.Wait()
	}()
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				c.push(buf[:n], conn)
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if typ == websocket.MessageBinary {
			if _, err := stdin.Write(data); err != nil {
				conn.Close(websocket.StatusInternalError, "decoder died")
				return
			}
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

