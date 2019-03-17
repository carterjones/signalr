package main

import (
	"log"
	"net/http"
	"net/url"

	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
)

func main() {
	// Start a local sample proxy.
	log.Println("Starting sample proxy...")
	ready := make(chan struct{})
	go startSampleProxy(ready)
	<-ready

	// Prepare a SignalR client.
	c := signalr.New(
		"socket.bittrex.com",
		"1.5",
		"/signalr",
		`[{"name":"c2"}]`,
		nil,
	)

	// Define message handler.
	msgHandler := func(msg signalr.Message) { log.Println(msg) }

	// Set up traffic proxying to localhost.
	proxyURL, err := url.Parse("http://127.0.0.1:8080")
	panicIfErr(err)
	roundtripper := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
	}
	c.HTTPClient.Transport = roundtripper

	// Start the connection.
	err = c.Run(msgHandler, panicIfErr)
	panicIfErr(err)

	// Subscribe to the USDT-BTC feed.
	err = c.Send(hubs.ClientMsg{
		H: "corehub",
		M: "SubscribeToExchangeDeltas",
		A: []interface{}{"USDT-BTC"},
		I: 1,
	})
	panicIfErr(err)

	// Wait indefinitely.
	select {}
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}
