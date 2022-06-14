package server

import (
	"crypto/rand"
	"fmt"

	"github.com/nofeaturesonlybugs/stomp"
)

// A SubscriptionPeer represents a peer to a Subscription.
//
// Each Subscription will have handles to one or more SubscriptionPeer instances and
// sends frames to them via the Send channel.
type SubscriptionPeer struct {
	// ID is an optional subscription ID specified by the peer.
	//
	// ID!=empty means outgoing messages should have a `subscription:ID` header so
	// clients can identify which of their subscriptions is originating messages.
	ID string

	// Subscription sends frames on the Send channel so they may reach the peer.
	Send chan<- stomp.Frame
}

// A Subscription represents a subscription destination in the server.  SubscriptionPeer
// instances can be added to the subscription and any messages sent to the subscription
// are forwarded to all peers.
type Subscription struct {
	// Destination is the subscription destination name in the server.
	Destination string

	// C is read by the Run method to add subscribers, remove subscribers, and send
	// messages to current subscribers.
	C chan interface{}

	// Logger is set by the Server type and is guaranteed to be non-nil.
	stomp.Logger

	// The following field(s) are maintained by SubscriptionManager.
	//
	// SubscriberCount is incremented/decremented as peers are subscribed/unsubscribed.
	// When SubscriberCount equals zero the SubscriptionManager knows it can close C
	// and remove the Subscription instance from its known subscriptions.
	SubscriberCount int
}

// Run is an infinite loop of subscription logic that ends when C is closed.
//
// C accepts the following types:
//	PeerSend          Sends a frame to all subscribed peers.
//
//	PeerSubscribe     Adds a peer to the list of subscribed peers.
//
//	PeerUnsubscribe   Removes a peer from the list of subscribed peers based on <Destination,ID>.
func (subscription Subscription) Run() {
	//
	var zero struct{}
	var err error
	peers := map[SubscriptionPeer]struct{}{}
	bufMessageID := make([]byte, 32)
	for event := range subscription.C {
		switch cmd := event.(type) {
		case PeerSend:
			if _, err = rand.Read(bufMessageID); err != nil {
				subscription.Infof("Subscription: generating %v: %v", stomp.HeaderMessageID, err.Error()) //TODO+NB Warning or Error?
				continue                                                                                  //TODO+NB Message is lost!
			}
			message := stomp.Frame{
				Command: stomp.CommandMessage,
				Headers: stomp.Headers{
					stomp.HeaderDestination: subscription.Destination,
					stomp.HeaderMessageID:   fmt.Sprintf("%x", bufMessageID),
				},
				Body: cmd.Frame.Body,
			}
			for peer := range peers {
				// TODO For subs with ID add `subscription:ID` header to outgoing frame.
				peer.Send <- message
			}
		case PeerSubscribe:
			if _, ok := peers[cmd.SubscriptionPeer]; ok {
				panic(fmt.Sprintf("peer already subscribed %v", cmd.SubscriptionPeer)) // TODO Remove? Err? Log?
			}
			peers[cmd.SubscriptionPeer] = zero
		case PeerUnsubscribe:
			delete(peers, cmd.SubscriptionPeer)
			if cmd.Await != nil { // if provided we close this channel to signal message originator
				close(cmd.Await)
			}
		}
	}
}
