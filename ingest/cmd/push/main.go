// push: broadcast an audio file (or any ffmpeg input) to a channel, like the browser does.
// usage: go run ./cmd/push -url ws://localhost:8090/ingest -ch main -token X input.mp3
package main

import (
	"context"
	"flag"
	"log"
	"net/url"
	"os"
	"os/exec"

	"github.com/coder/websocket"
)

func main() {
	base := flag.String("url", "ws://localhost:8090/ingest", "ingest websocket URL")
	ch := flag.String("ch", "main", "channel")
	token := flag.String("token", "", "broadcast token")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("usage: push [flags] <audio file or ffmpeg input>")
	}

	ctx := context.Background()
	q := url.Values{"ch": {*ch}, "token": {*token}}
	conn, _, err := websocket.Dial(ctx, *base+"?"+q.Encode(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.CloseNow()

	// -re: read at real-time speed, encode to webm/opus like a browser
	ff := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-re", "-i", flag.Arg(0),
		"-vn", "-c:a", "libopus", "-b:a", "48k", "-f", "webm", "-cluster_time_limit", "100", "pipe:1")
	ff.Stderr = os.Stderr
	out, _ := ff.StdoutPipe()
	if err := ff.Start(); err != nil {
		log.Fatal(err)
	}
	go func() { _, _, err := conn.Read(ctx); log.Printf("server closed: %v", err); ff.Process.Kill() }()

	buf := make([]byte, 4096)
	for {
		n, err := out.Read(buf)
		if n > 0 {
			if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
				log.Fatal(werr)
			}
		}
		if err != nil {
			break
		}
	}
	ff.Wait()
	conn.Close(websocket.StatusNormalClosure, "done")
}
