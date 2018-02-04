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

See the provided examples for how to use this library.
*/
package signalr
