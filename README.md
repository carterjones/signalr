[![GoDoc](https://godoc.org/github.com/carterjones/signalr?status.svg)](https://godoc.org/github.com/carterjones/signalr)
[![CircleCI](https://circleci.com/gh/carterjones/signalr.svg?style=svg)](https://circleci.com/gh/carterjones/signalr)
[![Go Report Card](https://goreportcard.com/badge/github.com/carterjones/signalr)](https://goreportcard.com/report/github.com/carterjones/signalr)
[![Maintainability](https://api.codeclimate.com/v1/badges/c561e13d50cdd11e97a1/maintainability)](https://codeclimate.com/github/carterjones/signalr/maintainability)
[![codecov](https://codecov.io/gh/carterjones/signalr/branch/master/graph/badge.svg)](https://codecov.io/gh/carterjones/signalr)

# Overview

This is my personal attempt at implementating the client side of the WebSocket
portion of the SignalR protocol. I use it for various virtual currency trading
platforms that use SignalR.

It supports CloudFlare-protected sites by default.

## Example basic usage

```go
package main

import (
	"log"

	"github.com/carterjones/signalr"
)

func main() {
	host := "myhost.not-real-tld"
	protocol := "some-protocol-version-123"
	endpoint := "/usually/something/like/this"
	connectionData := `{"custom":"data"}`

	// Prepare a SignalR client.
	c := signalr.New(host, protocol, endpoint, connectionData)

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
```

## Example complex usage

```go
package main

import (
	"log"

	"github.com/carterjones/signalr"
)

func main() {
	host := "myhost.not-real-tld"
	protocol := "some-protocol-version-123"
	endpoint := "/usually/something/like/this"
	connectionData := `{"custom":"data"}`

	// Prepare a SignalR client.
	c := signalr.New(host, protocol, endpoint, connectionData)

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
	msgs := make(chan signalr.Message)
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
}
```

# Documentation

- GoDoc: https://godoc.org/github.com/carterjones/signalr
- SignalR specification: https://docs.microsoft.com/en-us/aspnet/signalr/overview/
- Excellent technical deep dive of the protocol: https://blog.3d-logic.com/2015/03/29/signalr-on-the-wire-an-informal-description-of-the-signalr-protocol/

# Contribute

If anything is unclear or could be improved, please open an issue or submit a
pull request. Thanks!
