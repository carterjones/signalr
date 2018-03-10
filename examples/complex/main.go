package main

import (
	"log"

	"github.com/carterjones/signalr"
)

func main() {
	// Prepare a SignalR client.
	c := signalr.New(
		"fake-server.definitely-not-real",
		"1.5",
		"/signalr",
		`[{"name":"awesomehub"}]`,
		map[string]string{"custom-key": "custom-value"},
	)

	// Perform any optional modifications to the client here. Read the docs for
	// all the available options that are exposed via public fields.

	// Define message and error handlers.
	msgHandler := func(msg signalr.Message) { log.Println(msg) }
	panicIfErr := func(err error) {
		if err != nil {
			log.Panic(err)
		}
	}

	// Manually perform the initialization routine.
	err := c.Negotiate()
	panicIfErr(err)
	conn, err := c.Connect()
	panicIfErr(err)
	err = c.Start(conn)
	panicIfErr(err)

	// Begin the message reading loop.
	go c.ReadMessages(msgHandler, panicIfErr)

	// Wait indefinitely.
	select {}
}
