package main

import (
	"log"

	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
)

// For more extensive use cases and capabilities, please see
// https://github.com/carterjones/bittrex.

func main() {
	// Prepare a SignalR client.
	c := signalr.New(
		"socket.bittrex.com",
		"1.5",
		"/signalr",
		`[{"name":"c2"}]`,
		nil,
	)

	// Set the user agent to one that looks like a browser.
	c.Headers["User-Agent"] = "Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36"

	// Send note to user about CloudFlare.
	log.Println("Bypassing CloudFlare. This takes about 5 seconds.")

	// Define message and error handlers.
	msgHandler := func(msg signalr.Message) { log.Println(msg) }
	panicIfErr := func(err error) {
		if err != nil {
			log.Panic(err)
		}
	}

	// Start the connection.
	err := c.Run(msgHandler, panicIfErr)
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
