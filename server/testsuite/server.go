package testsuite

import (
	"fmt"
	"net"
	"sync"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/server"
)

// TestServer embeds the stomp server type and provides utility.
type TestServer struct {
	server.Server

	// List of connected clients
	Clients []MockClient

	// When tests create clients with calls to Consumer or Producer then the
	// returned client is also placed in the following slices.
	Consumers []MockClient
	Producers []MockClient

	// AsNetwork=true means the server is a network server and peers
	// connect as network clients.
	AsNetwork bool

	onceInit           sync.Once // only init server once
	onceListenAndServe sync.Once // only start network one time
}

// init sets initial values on server by setting any fields on Server that aren't explicitly set.
func (s *TestServer) init() {
	if s.Logger == nil {
		s.Logger = stomp.StdoutLogger
	}
	if s.NewSessionID == nil {
		sessionID := -1
		s.NewSessionID = func() string {
			sessionID++
			return fmt.Sprintf("session #%v", sessionID)
		}
	}
}

// Client connects a client to the server and returns the MockClient instance.
//
// The client's Start method is called with wg as its argument and a CONNECT frame
// is sent.
//
// AsNetwork=true means the underlying client is a net.Conn.
// AsNetwork=false means the underlying client is created from io.Pipe.
func (s *TestServer) Client(wg *sync.WaitGroup) (MockClient, error) {
	s.onceInit.Do(s.init)
	var client MockClient
	//
	if s.AsNetwork {
		// Might need to start the server
		ErrorC := make(chan error, 1)
		s.onceListenAndServe.Do(func() {
			ErrorC <- s.ListenAndServe()
		})
		select {
		case err := <-ErrorC:
			if err != nil {
				return MockClient{}, err
			}
		default:
		}
		//
		conn, err := net.Dial("tcp", s.Addr)
		if err != nil {
			return MockClient{}, err
		}
		client = MockClient{
			Peer: stomp.Peer{
				R: conn,
				W: conn,
			},
		}
	} else {
		client = MockClient{
			Peer: s.Pipe(),
		}
	}
	//
	client.Start(wg)
	//
	if err := client.Connect(fmt.Sprintf("session #%v", len(s.Clients))); err != nil {
		client.Shutdown()
		return MockClient{}, err
	}
	s.Clients = append(s.Clients, client)
	return client, nil
}

// Consumer is identical to Client except the returned client is also added to the
// Consumers slice of the test server.
func (s *TestServer) Consumer(wg *sync.WaitGroup) (MockClient, error) {
	client, err := s.Client(wg)
	s.Consumers = append(s.Consumers, client)
	return client, err
}

// Producer is identical to Client except the returned client is also added to the
// Producers slice of the test server.
func (s *TestServer) Producer(wg *sync.WaitGroup) (MockClient, error) {
	client, err := s.Client(wg)
	s.Producers = append(s.Producers, client)
	return client, err
}
