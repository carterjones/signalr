package main

import (
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
)

const (
	proxyAuthHeader = "Proxy-Authorization"
	validUser       = "myuser"
	validPass       = "mypassword"
)

// Copied from
// https://github.com/elazarl/goproxy/blob/2ce16c963a8ac5bd6af851d4877e38701346983f/examples/cascadeproxy/main.go#L30-L50
func getBasicAuth(req *http.Request) (username, password string, ok bool) {
	log.Println("[proxy] headers received:", req.Header)
	auth := req.Header.Get(proxyAuthHeader)
	if auth == "" {
		return "", "", false
	}

	const prefix = "Basic "
	if !strings.HasPrefix(auth, prefix) {
		return "", "", false
	}
	c, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
	if err != nil {
		return "", "", false
	}
	cs := string(c)
	s := strings.IndexByte(cs, ':')
	if s < 0 {
		return "", "", false
	}
	return cs[:s], cs[s+1:], true
}

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
			user, pass, ok := getBasicAuth(r)
			if !ok {
				log.Println("[proxy] requires proxy authorization. rejecting.")
				_, err := client.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n\r\n"))
				panicIfErr(err)
				err = client.Close()
				panicIfErr(err)
				return
			}

			if user != validUser || pass != validPass {
				log.Println("[proxy] unauthorized. rejecting.")
				_, err := client.Write([]byte("HTTP/1.1 401 Unauthorized\r\n\r\n"))
				panicIfErr(err)
				err = client.Close()
				panicIfErr(err)
				return
			}

			log.Println("[proxy] received authorized request. handling.")
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
