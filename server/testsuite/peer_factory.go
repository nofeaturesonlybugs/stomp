package testsuite

import (
	"net"
	"sync"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/server"
)

// PeerFactory facilitates creating peers for our stomp/server tests.
//
// By default Make returns peers created with stomp.Pipe and the peers are
// connected by the types memory pipes; see io.Pipe.
//
// By calling AsNetwork the peer factory will start a net.Listener and peers
// created by Make will be created by a net.Dial (remote peer) and Listener.Accept (local peer).
type PeerFactory struct {
	// ServerC, ServerSignals, Logger, and WaitGroup are passed to the peers during Make.
	ServerC       chan interface{}
	ServerSignals server.ServerSignals
	stomp.Logger
	WaitGroup *sync.WaitGroup

	// StartLocal=true means Make will call Start method on local stomp.Peer.
	// StartRemote=true means Make will call Start method on remote stomp.Peer
	StartLocal  bool
	StartRemote bool

	// Locals are the local stomp.Peers and remotes are the remote stomp.Peers.
	Locals  []stomp.Peer
	Remotes []stomp.Peer

	// Peers are the server.Peer instances tied to elements in Locals.
	Peers []server.Peer

	// Listener is created and set by the AsNetwork method.
	Listener net.Listener
}

// Make returns the remote stomp.Peer and the local server.Peer.
func (factory *PeerFactory) Make() (Remote stomp.Peer, Peer server.Peer, err error) {
	var l stomp.Peer
	if factory.Listener == nil {
		// as pipes
		l, Remote = stomp.Pipe()
	} else {
		// as net conns
		var lconn, rconn net.Conn
		var errAccept error
		if factory.WaitGroup != nil {
			factory.WaitGroup.Add(1)
		}
		acceptC := make(chan stomp.Signal)
		go func() {
			defer func() {
				if factory.WaitGroup != nil {
					factory.WaitGroup.Done()
				}
				close(acceptC)
			}()
			lconn, errAccept = factory.Listener.Accept()
		}()
		rconn, err = net.Dial(factory.Listener.Addr().Network(), factory.Listener.Addr().String())
		if err != nil {
			return
		}
		<-acceptC // need to block until the goroutine returns otherwise errAccept and lconn may not yet  be set
		if err = errAccept; err != nil {
			return
		}
		l = stomp.Peer{
			R: lconn,
			W: lconn,
		}
		Remote = stomp.Peer{
			R: rconn,
			W: rconn,
		}
	}
	if factory.StartLocal {
		l.Start(factory.WaitGroup)
	}
	if factory.StartRemote {
		Remote.Start(factory.WaitGroup)
	}
	factory.Locals = append(factory.Locals, l)
	factory.Remotes = append(factory.Remotes, Remote)
	Peer = server.Peer{
		Peer:          l,
		Session:       stomp.SessionID(),
		Subscriptions: map[server.PeerSubscription]struct{}{},
		ServerC:       factory.ServerC,
		ServerSignals: factory.ServerSignals,
		Logger:        factory.Logger,
	}
	factory.Peers = append(factory.Peers, Peer)
	return
}

// AsNetwork configures the peer factory to create net worked peers using net.Listener and net.Dial.
func (factory *PeerFactory) AsNetwork() error {
	var err error
	factory.Listener, err = net.Listen("tcp", "127.0.0.1:")
	return err
}
