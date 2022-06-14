package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
	"github.com/nofeaturesonlybugs/stomp/server/events"
)

// ServerSignals allows granular control and notifications of a Server's lifecycle.
type ServerSignals struct {
	// SigShutdown is closed when the server has been told to begin shutting down.
	SigShutdown <-chan stomp.Signal

	// SigShutdownPeers is closed when the server needs to signal all current peers to begin shutting down.
	SigShutdownPeers <-chan stomp.Signal

	// AwaitMessagesConsumed is closed when the server has consumed all remaining incoming messages from peers.
	AwaitMessagesConsumed <-chan stomp.Signal

	// AwaitSubscriptions is closed when all subscription goroutines have finished.
	AwaitSubscriptions <-chan stomp.Signal

	// AwaitStopped is closed when the server is stopped.
	AwaitStopped <-chan stomp.Signal

	// internal handles are necessary so server can close() them.
	sigShutdown           chan stomp.Signal
	sigShutdownPeers      chan stomp.Signal
	awaitMessagesConsumed chan stomp.Signal
	awaitSubscriptions    chan stomp.Signal
	awaitStopped          chan stomp.Signal
}

// newServerSignals creates the server signals type.
func newServerSignals() ServerSignals {
	sigShutdown := make(chan stomp.Signal)
	sigShutdownPeers := make(chan stomp.Signal)
	awaitMessagesConsumed := make(chan stomp.Signal)
	awaitSubscriptions := make(chan stomp.Signal)
	awaitStopped := make(chan stomp.Signal)
	return ServerSignals{
		SigShutdown:           sigShutdown,
		SigShutdownPeers:      sigShutdownPeers,
		AwaitMessagesConsumed: awaitMessagesConsumed,
		AwaitSubscriptions:    awaitSubscriptions,
		AwaitStopped:          awaitStopped,
		// internal handles
		sigShutdown:           sigShutdown,
		sigShutdownPeers:      sigShutdownPeers,
		awaitMessagesConsumed: awaitMessagesConsumed,
		awaitSubscriptions:    awaitSubscriptions,
		awaitStopped:          awaitStopped,
	}
}

// Server is a STOMP server.
//
// Server should not be copied once started.
type Server struct {
	// Addr specifies the TCP address for the server to listen on
	// in the form of "host:port".
	//
	// If empty then a random port is used with 127.0.0.1 and this
	// field is updated accordingly.
	Addr string

	// Events is an optional channel for the server to push event notifications into.
	Events chan<- interface{}

	// Signals allows granular control and notification of the server's lifecycle.
	Signals ServerSignals

	// TLSConfig specifies an optional TLS configuration.
	TLSConfig *tls.Config

	// Logger!=nil means messages and information are logged to the
	// provided Logger.
	stomp.Logger

	// NewSessionID is the provider for client session IDs.
	//
	// NewSessionID=nil means SessionID will be used as this provider.
	NewSessionID func() string

	once           sync.Once       // Method stomp should only be called once.
	incomingPeerCh chan stomp.Peer // Methods Pipe+Serve send new peers into this channel.

	wg sync.WaitGroup // TODO Remove from struct, make local to stomp method?
}

// stomp spins up all the goroutines necessary for the STOMP server to work.
func (srv *Server) stomp() {
	var log stomp.Logger
	if log = srv.Logger; log == nil {
		log = stomp.NilLogger
	}
	//
	// Methods Pipe+Serve push newly joined peers into this channel; channel is read below.
	srv.incomingPeerCh = make(chan stomp.Peer, 32)
	//
	// Events is an optional channel to broadcast server events to; if nil we create our own
	// events channel and self-consume.
	var wgConsume sync.WaitGroup
	var consumeEventsCh <-chan interface{}
	var eventsCh chan<- interface{}
	if eventsCh = srv.Events; eventsCh == nil {
		c := make(chan interface{})
		consumeEventsCh, eventsCh = c, c
	}
	//
	// NewSessionID returns a new session ID.
	var NewSessionID func() string
	if NewSessionID = srv.NewSessionID; NewSessionID == nil {
		NewSessionID = stomp.SessionID
	}
	//
	// Server signals.
	srv.Signals = newServerSignals()
	//
	// The following channels are shared between goroutines in this method but
	// are not needed outside of this method.
	// TODO Configurable channel buffer sizes.
	//
	// consumer: main server goroutine started within this function
	// producer: VerifyPeer func (defined below) after peer authentication succeeds
	verifiedPeerC := make(chan Peer, 32) // adds peer to known peers
	//
	// consumer: main server goroutine started within this function
	// producer: peer goroutine
	peersC := make(chan interface{}, 32)

	// AuthenticatePeer is called when a Peer first joins the server.  AuthenticatePeer
	// reads an initial STOMP frame, which must be a CONNECT or STOMP frame,
	// and authenticates any provided credentials.
	//
	// If the Peer authenticates correctly it is sent on acceptedPeerCh where the main
	// server goroutine picks it up to continue processing.
	//
	// If the Peer fails to authenticate or has errors in its communications it is shutdown
	// and dropped.
	AuthenticatePeer := func(peer Peer) {
		var frame stomp.Frame
		var err error
		// TODO Should add case with a timeout. Give client D duration to submit a frame and then close on them
		select {
		case frame = <-peer.Receive:
			if frame.Command != stomp.CommandConnect && frame.Command != stomp.CommandStomp {
				body := fmt.Sprintf("Expected %v or %v", stomp.CommandConnect, stomp.CommandStomp)
				peer.SendAndShutdown(frames.Error("invalid command", body, frame))
				return
			}
		case err = <-peer.Error:
			if !errors.Is(err, io.EOF) {
				peer.Send <- frames.Error("invalid frame", err.Error(), frames.Empty)
			}
			_ = peer.Shutdown()
			return
		case <-srv.Signals.SigShutdown:
			peer.SendAndShutdown(frames.Error("shutdown", "shutdown", frames.Empty)) // TODO better message.
			return
		}
		//
		// TODO Authentication
		peer.Subscriptions = map[PeerSubscription]struct{}{}
		peer.Session = NewSessionID()
		peer.ServerC = peersC
		peer.ServerSignals = srv.Signals
		//
		// Send CONNECTED frame
		peer.Send <- frames.Connected(peer.Session)
		//
		// Pass to main stomp goroutine.
		verifiedPeerC <- peer
	}

	//
	// self consume events if necessary
	if consumeEventsCh != nil {
		wgConsume.Add(1)
		go func() {
			defer wgConsume.Done()
			for range consumeEventsCh {
			}
		}()
	}
	//
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()

		// Subscriptions have their own waitgroup.
		var wgSubscriptions sync.WaitGroup

		// This is the only goroutine that accesses the subscription manager's values
		// and as such needs no extra locking or concurrency mechanisms.
		subscriptions := SubscriptionManager{
			Subscriptions: map[string]Subscription{},
			EventsC:       eventsCh,
			Logger:        log,
		}
		nPeers := 0        // track number of active peers
		nPeersReading := 0 // track number of peers with active readers; when readers==0 peers will not send anymore messages to server
	MainLoop:
		for {
			select {
			case p := <-srv.incomingPeerCh:
				// Pipe and Serve serve produce to incomingPeerCh; peer requires authentication.
				// p is a stomp.Peer which must be a) started and b) elevated to Peer defined in this package.
				p.Start(&srv.wg)
				peer := Peer{
					Peer:   p,
					Logger: log,
				}
				srv.wg.Add(1)
				go func() {
					defer srv.wg.Done()
					AuthenticatePeer(peer)
				}()

			case peer := <-verifiedPeerC:
				// An accepted Peer is authenticated.
				nPeers++
				nPeersReading++
				srv.wg.Add(1)
				go func(peer Peer) {
					defer srv.wg.Done()
					peer.Run()
				}(peer)

			case event := <-peersC:
				switch cmd := event.(type) {
				case PeerClosedReader:
					nPeersReading--
				case PeerDisconnected:
					nPeers--
				case PeerSend:
					// TODO Do we need to know if peer tried to send on unknown subscription?
					subscriptions.Send(cmd)
				case PeerSubscribe:
					subscriptions.Subscribe(cmd, &wgSubscriptions)
				case PeerUnsubscribe:
					// TODO Do we need to know if a peer tries to unsubscribe from unkown subscription?
					subscriptions.Unsubscribe(cmd)
				default:
					log.Infof("stomp.server: unhandled peer event: %T %v", event, event)
				}

			case <-srv.Signals.SigShutdown:
				break MainLoop
			}
		}
		//
		// Shutdown logic
		close(srv.Signals.sigShutdownPeers) // signal all peers to begin shutting down
		if nPeersReading == 0 {
			close(srv.Signals.awaitMessagesConsumed)
		}
		for nPeers != 0 || subscriptions.Len() != 0 {
			select {
			case p := <-srv.incomingPeerCh:
				p.SendAndShutdown(frames.Error("server shutting down", "The server is shutting down.", frames.Empty)) // TODO Better message.
			case peer := <-verifiedPeerC:
				peer.SendAndShutdown(frames.Error("server shutting down", "The server is shutting down.", frames.Empty)) // TODO Better message.
			case event := <-peersC:
				switch cmd := event.(type) {
				case PeerClosedReader:
					if nPeersReading = nPeersReading - 1; nPeersReading == 0 {
						close(srv.Signals.awaitMessagesConsumed)
					}
				case PeerDisconnected:
					nPeers--
				case PeerSend:
					// TODO Do we need to know if peer tried to send on unknown subscription?
					subscriptions.Send(cmd)
				case PeerSubscribe:
					subscriptions.Subscribe(cmd, &wgSubscriptions)
				case PeerUnsubscribe:
					// TODO Do we need to know if a peer tries to unsubscribe from unkown subscription?
					subscriptions.Unsubscribe(cmd)
				default:
					log.Infof("stomp.server: shutdown: unhandled peer event: %T %v", event, event)
				}
			}
		}
		//
		// When all subscriptions are stopped we can close the AwaitSubscriptions channel and allow
		// peers to continue their shutdown knowing all messages have been delievered.
		wgSubscriptions.Wait()
		close(srv.Signals.awaitSubscriptions)
		//
		go func() {
			srv.wg.Wait()
			//
			eventsCh <- events.ServerStop{}
			close(eventsCh)
			wgConsume.Wait()
			//
			close(srv.Signals.awaitStopped)
		}()
	}()
}

// Pipe creates a pair of Peers whose R and W fields are linked to each other via
// pipes created by calls to io.Pipe.
//
// One Peer is inserted into the server and the other is returned.  The returned Peer
// can be used to communicate with the Server as if it had joined via network connection.
func (srv *Server) Pipe() stomp.Peer {
	srv.once.Do(srv.stomp)
	server, client := stomp.Pipe()
	srv.incomingPeerCh <- server
	return client
}

// ListenAndServe listens on the TCP network address Server.Addr and then calls Serve
// to accept incoming connections.
//
// If Server.Addr is blank then "127.0.0.1:" is used and Server.Addr is updated with
// the listener's address.
//
// TODO Accepted connections are configured to enable TCP keep-alives.
// TODO Unlike ListenAndServe in standard library net/http this method is non-blocking
// and returns a nil error if the server is running or an error if it is not.
func (srv *Server) ListenAndServe() error {
	var listener net.Listener
	var addr string
	var err error
	//
	if addr = srv.Addr; addr == "" {
		addr = "127.0.0.1:"
	}
	//
	if srv.TLSConfig != nil {
		if listener, err = tls.Listen("tcp", addr, srv.TLSConfig); err != nil {
			return err
		}
	} else {
		if listener, err = net.Listen("tcp", addr); err != nil {
			return err
		}
	}
	//
	srv.Addr = listener.Addr().String()
	srv.Serve(listener)
	//
	return nil
}

// Serve starts necessary internal goroutines to accept and handle connections on
// the net.Listener l and then returns.
func (srv *Server) Serve(l net.Listener) {
	srv.once.Do(srv.stomp)
	//
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-srv.Signals.SigShutdown:
					return
				default:
					// fmt.Println(fmt.Errorf("stomp: accept error: %w", err)) // TODO log it
					continue
				}
			}
			//
			peer := stomp.Peer{
				// TODO Might want to allow for SetReadDeadline and SetWriteDeadline.
				//      However -- due to the Peer's implementation I'm not sure it's
				//      necessary.
				R: conn,
				W: conn,
			}
			srv.incomingPeerCh <- peer
		}
	}()
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		<-srv.Signals.SigShutdown
		if err := l.Close(); err != nil {
			// fmt.Printf("Server.Accept: l.Close=%v\n", err) // TODO log it
		}
	}()
}

// Shutdown gracefully shuts down the server.
func (srv *Server) Shutdown() error { // TODO No error conditions yet.
	close(srv.Signals.sigShutdown)
	<-srv.Signals.AwaitStopped
	return nil
}
