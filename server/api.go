package server

import "github.com/nofeaturesonlybugs/stomp"

// PeerClosedReader is generated by a peer when its reader is closed.
type PeerClosedReader stomp.Signal

// PeerDisconnected is generated by a peer when it disconnects from the server.
type PeerDisconnected stomp.Signal

// PeerSend is generated by a peer when it wishes to send a message to a destination.
type PeerSend struct {
	// Destination is the subscription destination name.
	Destination string

	// Frame is the original Frame that generated this event.
	Frame stomp.Frame
}

// PeerSubscribe is generated by a peer when it wishes to subscribe to a destination.
type PeerSubscribe struct {
	// Destination is the subscription destination name.
	Destination string

	SubscriptionPeer
}

// PeerUnsubscribe is generated by a peer when it wishes to unsubscribe from a destination.
type PeerUnsubscribe struct {
	// Destination is the subscription destination name.
	Destination string

	SubscriptionPeer

	// Await!=nil means the subscription will close the Await channel to indicate the
	// request has been fulfilled.
	Await chan<- stomp.Signal
}
