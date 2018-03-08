package main

import (
	"log"

	"github.com/carterjones/signalr"
	"github.com/carterjones/signalr/hubs"
)

func main() {
	// Prepare a SignalR client.
	c := signalr.New(
		"socket.bittrex.com",
		"1.5",
		"/signalr",
		`[{"name":"corehub"}]`,
		nil,
	)

	// Set the user agent to one that looks like a browser.
	c.Headers["User-Agent"] = "Mozilla/5.0 (Windows NT 6.1) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/41.0.2228.0 Safari/537.36"

	log.Println("Bypassing CloudFlare. This takes about 5 seconds.")

	// Start the connection.
	msgs, errs, err := c.Run()
	if err != nil {
		log.Panic(err)
	}

	// Subscribe to the USDT-BTC feed.
	err = c.Send(hubs.ClientMsg{
		H: "corehub",
		M: "SubscribeToExchangeDeltas",
		A: []interface{}{"USDT-BTC"},
		I: 1,
	})
	if err != nil {
		log.Panic(err)
	}

	// Process messages and errors.
	go func() {
		for {
			select {
			case msg := <-msgs:
				// Handle the message.
				log.Println(msg)
			case err := <-errs:
				// Handle the error.
				log.Panic(err)
			}
		}
	}()

	select {}
}
