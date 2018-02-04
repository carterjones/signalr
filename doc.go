/*
Package signalr provides the client side implementation of the WebSocket
portion of the SignalR protocol.

First things first: this was almost entirely written using
https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/
as a reference guide. It is an excellent technical write-up. Many thanks to
Pawel Kadluczka for writing that and sharing it with the public. If you want
deep-dive technical details of how this all works, read that blog. I won't
try to replicate it here.

At a high level, the WebSocket portion of SignalR goes through the following steps:

	- negotiate: use HTTP/HTTPS to get connection info for how to connect to the
	  websocket endpoint
	- connect: attempt to connect to the websocket endpoint
	- start: make the WebSocket connection usable by SignalR connections

The easiest way to start a connection is in the following way:

	// Prepare a SignalR client.
	c = signalr.New(host, protocol, endpoint, connectionData)

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

If you want to control the individual steps of the connection sequence, you
can also execute them manually in the following way:

	// Prepare a SignalR client.
	c = signalr.New(host, protocol, endpoint, connectionData)

	// Perform any optional modifications to the client here. Read the docs for
	// all the available options that are exposed via public fields.

	// Manually perform the initialization routine.
	err := c.Negotiate()
	if err != nil {
		log.Panic(err)
	}
	conn, err := c.Connect()
	if err != nil {
		log.Panic(err)
	}
	err = c.Start(conn)
	if err != nil {
		log.Panic(err)
	}

	// Create message and error channels.
	msgs := make(chan Message)
	errs := make(chan error)

	// Begin the message reading loop.
	go c.ReadMessages(msgs, errs)

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
*/
package signalr
