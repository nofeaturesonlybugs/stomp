package server

import (
	"errors"
	"fmt"
	"io"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
)

// PeerSubscription allows a Peer to internally track its active subscriptions.
type PeerSubscription struct {
	// Destination is the subscription destination name in the server.
	Destination string

	// ID is an optional subscription ID specified by the peer.
	//
	// ID!=empty means outgoing messages should have a `subscription:ID` header so
	// clients can identify which of their subscriptions is originating messages.
	ID string
}

// Peer contains an embedded stomp.Peer and then extraneous information needed
// by the server.
type Peer struct {
	stomp.Peer

	// Session is the peer's unique session ID.
	Session string

	// Subscriptions is the peer's registry of its active subscriptions.
	Subscriptions map[PeerSubscription]struct{}

	// ServerC is read by the server.
	//
	// The peer can push data, notifications, and events into this channel
	// to signal the server; for example SUBSCRIBE and UNSUBSCRIBE requests.
	//
	// Peers should not close this channel.
	ServerC chan<- interface{} // TODO Possibly replace with interface type

	// ServerSignals allows the peer to listen in on the server's lifecycle.
	ServerSignals ServerSignals

	// Logger is set by the Server type and is guaranteed to be non-nil.
	stomp.Logger // TODO Final implementation may not need logging; convenient for development though.
}

// Run is an infinite loop of peer logic that ends when the server shuts down
// or the remote endpoint of the peer closes.
func (peer Peer) Run() {
	var serverShutdownC <-chan stomp.Signal = peer.ServerSignals.SigShutdownPeers
	var framesC <-chan stomp.Frame = peer.Receive
	var errC <-chan error = peer.Error
	//
	for framesC != nil || errC != nil {
		select {
		case frame, open := <-framesC:
			if !open {
				framesC = nil
				continue
			}
			if err := peer.HandleFrame(frame); err != nil {
				peer.Send <- frames.Error(err.Error(), err.Error(), frame)
			}
		case err, open := <-errC:
			if !open {
				errC = nil
				continue
			}
			if !errors.Is(err, io.EOF) {
				//TODO Possibly send ERROR frame to client.
			}
		case <-serverShutdownC:
			serverShutdownC = nil // stops this case from firing again
			// Signal peer to shut down its reader but stay in this loop until it does so.
			// Since the peer may not have any errors during this process we also set errC to nil
			// to ensure we actually exit the loop.
			close(peer.Signals.StopReader)
			errC = nil
		}
	}
	<-peer.Signals.AwaitReader         // Unblocks when our reader is done; this peer will send no more messages.
	peer.ServerC <- PeerClosedReader{} // Notify server the peer's reader is closed.
	if serverShutdownC == nil {        // Only wait for all messages to be delivered during server shutdown event.
		<-peer.ServerSignals.AwaitMessagesConsumed // Unblocks when server has sent all remaining messages.
	}
	//
	// Unsubscribe from all active subscriptions
	for subscription := range peer.Subscriptions {
		await := make(chan stomp.Signal)
		peer.ServerC <- PeerUnsubscribe{
			Destination: subscription.Destination,
			SubscriptionPeer: SubscriptionPeer{
				ID:   subscription.ID,
				Send: peer.Send,
			},
			Await: await,
		}
		<-await
		delete(peer.Subscriptions, subscription)
	}
	//
	close(peer.Send) // Required otherwise AwaitSender doesn't signal.
	<-peer.Signals.AwaitSender
	<-peer.Signals.AwaitClosed
	peer.ServerC <- PeerDisconnected{}
}

// HandleFrame is called when Run receives a frame from the remote endpoint.
func (peer Peer) HandleFrame(f stomp.Frame) error {
	var dest string
	switch stomp.Command(f.Command) {
	// case stomp.CommandAck:// TODO
	// case stomp.CommandDisconnect:// TODO
	case stomp.CommandSend:
		if dest = f.Headers[stomp.HeaderDestination]; dest == "" {
			return fmt.Errorf("%w: %v", stomp.ErrMissingHeader, stomp.HeaderDestination)
		}
		peer.ServerC <- PeerSend{
			Destination: dest,
			Frame:       f,
		}
	case stomp.CommandSubscribe:
		if dest = f.Headers[stomp.HeaderDestination]; dest == "" {
			return fmt.Errorf("%w: %v", stomp.ErrMissingHeader, stomp.HeaderDestination)
		}
		subscription := PeerSubscription{
			Destination: dest,
			ID:          f.Headers[stomp.HeaderID],
		}
		if _, ok := peer.Subscriptions[subscription]; ok {
			return fmt.Errorf("%w: destination=%v id=%v", stomp.ErrDuplicateSubscription, subscription.Destination, subscription.ID)
		}
		peer.Subscriptions[subscription] = struct{}{}
		peer.ServerC <- PeerSubscribe{
			Destination: dest,
			SubscriptionPeer: SubscriptionPeer{
				ID:   subscription.ID,
				Send: peer.Send,
			},
		}
	case stomp.CommandUnsubscribe:
		if dest = f.Headers[stomp.HeaderDestination]; dest == "" {
			return fmt.Errorf("%w: %v", stomp.ErrMissingHeader, stomp.HeaderDestination)
		}
		subscription := PeerSubscription{
			Destination: dest,
			ID:          f.Headers[stomp.HeaderID],
		}
		if _, ok := peer.Subscriptions[subscription]; ok {
			await := make(chan stomp.Signal)
			peer.ServerC <- PeerUnsubscribe{
				Destination: dest,
				SubscriptionPeer: SubscriptionPeer{
					ID:   subscription.ID,
					Send: peer.Send,
				},
				Await: await,
			}
			<-await
			delete(peer.Subscriptions, subscription)
		}
	}
	return nil
}
