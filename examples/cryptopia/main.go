package main

import (
	"log"

	"github.com/carterjones/signalr"
)

func main() {
	// Prepare a SignalR client.
	c := signalr.New(
		"www.cryptopia.co.nz",
		"1.5",
		"/signalr",
		`[{"name":"notificationhub"}]`,
		nil,
	)

	// Start the connection.
	msgs, errs, err := c.Run()
	if err != nil {
		log.Panic(err)
	}

	// Process messages and errors.
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
}
