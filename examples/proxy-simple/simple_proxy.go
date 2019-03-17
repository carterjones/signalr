package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/elazarl/goproxy"
)

// Inspired by http://blog.evilissimo.net/simple-port-fowarder-in-golang
func forward(conn io.ReadWriteCloser, host string) {
	client, err := net.Dial("tcp", host)
	if err != nil {
		log.Fatalf("dial failed: %v", err)
	}
	go func() {
		defer func() {
			err := client.Close()
			panicIfErr(err)
		}()
		defer func() {
			err := conn.Close()
			panicIfErr(err)
		}()
		_, err := io.Copy(client, conn)
		panicIfErr(err)
	}()
	go func() {
		defer func() {
			err := client.Close()
			panicIfErr(err)
		}()
		defer func() {
			err := conn.Close()
			panicIfErr(err)
		}()
		_, err := io.Copy(conn, client)
		panicIfErr(err)
	}()
}

func startSampleProxy(ready chan<- struct{}) {
	// Prepare a sample proxy.
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	proxy.OnRequest().HijackConnect(
		func(r *http.Request, client net.Conn, ctx *goproxy.ProxyCtx) {
			log.Println("[proxy] received request. handling.")
			forward(client, r.URL.Host)
			log.Println("[proxy] done handling.")
		})

	// Kick off a goroutine that waits for a short time and then sends a "ready"
	// signal.
	go func() {
		time.Sleep(time.Second * 1)
		ready <- struct{}{}
	}()

	// Start the sample proxy.
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", proxy))
}
