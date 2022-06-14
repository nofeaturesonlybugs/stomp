package server_test

import (
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
	"github.com/nofeaturesonlybugs/stomp/server"
	"github.com/nofeaturesonlybugs/stomp/server/events"
	"github.com/nofeaturesonlybugs/stomp/server/testsuite"
)

func TestServer_ListenAndServe(t *testing.T) {
	chk := assert.New(t)
	//
	srv := server.Server{
		Logger: stomp.StdoutLogger,
	}
	err := srv.ListenAndServe()
	chk.NoError(err)
	//
	chk.NotEmpty(srv.Addr)
	//
	c, err := net.Dial("tcp", srv.Addr)
	chk.NoError(err)
	time.Sleep(100 * time.Microsecond)
	err = c.Close()
	chk.NoError(err)
	//
	err = srv.Shutdown()
	chk.NoError(err)
}

func TestServer_ConnectFrame(t *testing.T) {
	sessionID := 0
	NewSessionID := func() string {
		sessionID++
		return fmt.Sprintf("session #%v", sessionID)
	}
	//
	type ConnectTest struct {
		Name   string
		Write  []byte
		Expect stomp.Frame
		Error  error
	}
	tests := []ConnectTest{
		{
			Name:  "good",
			Write: []byte("CONNECT\nlogin:USERNAME\npasscode:PASSWORD\n\n\x00"),
			Expect: stomp.Frame{
				Command: "CONNECTED",
				Headers: stomp.Headers{
					"session": "session #1",
				},
			},
			Error: nil,
		},
		{
			Name:  "eof",
			Error: nil,
		},
		{
			Name:  "bad command",
			Write: []byte("BAD-COMMAND\nlogin:USERNAME\npasscode:PASSWORD\n\n\x00"),
			Expect: stomp.Frame{
				Command: "ERROR",
				Headers: stomp.Headers{
					"message": "invalid command",
				},
				Body: []byte("The frame\n----\nBAD-COMMAND\nlogin:USERNAME\npasscode:PASSWORD\n\n\x00\n----\nExpected CONNECT or STOMP\n"),
			},
			Error: nil,
		},
		{
			Name:  "bad header",
			Write: []byte("CONNECT\nloginUSERNAME\npasscodePASSWORD\n\n\x00"),
			Expect: stomp.Frame{
				Command: "ERROR",
				Headers: stomp.Headers{
					"message": "invalid frame",
				},
				Body: []byte("stomp: peer reader: stomp: invalid frame: header missing colon: loginUSERNAME\n"),
			},
			Error: nil,
		},
	}
	//
	srv := server.Server{
		Logger:       stomp.StdoutLogger,
		NewSessionID: NewSessionID,
	}
	defer srv.Shutdown()
	//
	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			chk := assert.New(t)
			//
			var n int
			var err error
			p := srv.Pipe()
			if len(test.Write) > 0 {
				n, err = p.W.Write(test.Write)
				chk.NoError(err)
				chk.Equal(len(test.Write), n)
				//
				parser := stomp.NewParser(p.R)
				f, err := parser.Frame()
				chk.ErrorIs(err, test.Error)
				if contentLength := len(test.Expect.Body); contentLength > 0 {
					test.Expect.Headers[stomp.HeaderContentLength] = strconv.Itoa(contentLength)
				}
				chk.Equal(test.Expect, f)
			}
			//
			err = p.Close()
			chk.NoError(err)
		})
	}
}

func TestServer_PeerDisconnectStopsSubscriptions(t *testing.T) {
	// Clients connect and establish N subscriptions.
	// Producer connects and begins sending messages on the N subscriptions.
	//
	// When a peer has received one message per subscription it disconnects without unsubscribing.
	//
	// Expect:  The server will clean up after the client (which disconnected without unsubscribing)
	//          in a way that doesn't result in panics (send on closed channel for example) and shuts
	//          down the internal subscription threads.
	//
	const Topic string = "/topic/peer-disconnect"
	type TestCase struct {
		Clients       int
		Subscriptions int
		Send          int
	}
	tests := []TestCase{
		{Clients: 1, Subscriptions: 1, Send: 1},
		{Clients: 1, Subscriptions: 3, Send: 1},
		{Clients: 1, Subscriptions: 5, Send: 10},
		{Clients: 1, Subscriptions: 5, Send: 100},
		//
		{Clients: 5, Subscriptions: 1, Send: 1},
		{Clients: 5, Subscriptions: 3, Send: 1},
		{Clients: 5, Subscriptions: 5, Send: 10},
		{Clients: 5, Subscriptions: 5, Send: 100},
		//
		{Clients: 20, Subscriptions: 1, Send: 1},
		{Clients: 20, Subscriptions: 3, Send: 1},
		{Clients: 20, Subscriptions: 5, Send: 10},
		{Clients: 20, Subscriptions: 10, Send: 20},
		//
		{Clients: 200, Subscriptions: 50, Send: 1000},
	}
	// Consumer connects a consumer to the server and begins message consumption.
	Consumer := func(srv *testsuite.TestServer, subscriptions testsuite.Subscriptions, wg *sync.WaitGroup) (ReadyC chan struct{}, ErrorC chan error, err error) {
		ReadyC, ErrorC = make(chan struct{}), make(chan error, 1)
		consumer, err := srv.Client(wg)
		if err != nil {
			close(ErrorC)
			close(ReadyC)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// get at least one message per topic...
			want := subscriptions.NewCountMap()
			for _, s := range subscriptions {
				consumer.Send <- frames.Subscribe(s.Topic, "", "")
			}
			close(ReadyC)
			for frame := range consumer.Receive {
				delete(want, frame.Headers[stomp.HeaderDestination])
				if len(want) == 0 { //...and then close
					break
				}
			}
			ErrorC <- consumer.Shutdown()
			close(ErrorC)
		}()
		return
	}
	// Producer creates the message producer.
	Producer := func(srv *testsuite.TestServer, subscriptions testsuite.Subscriptions, wg *sync.WaitGroup) (CloseC chan struct{}, ErrorC chan error, err error) {
		CloseC, ErrorC = make(chan struct{}), make(chan error, 1)
		producer, err := srv.Client(wg)
		if err != nil {
			close(CloseC)
			close(ErrorC)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			produce := true
			for produce {
				select {
				case _, produce = <-CloseC:
				default:
					for _, s := range subscriptions {
						producer.Send <- s.SendFrame
					}
				}
			}
			ErrorC <- producer.Shutdown()
			close(ErrorC)
		}()
		return
	}
	Fn := func(test TestCase, network bool, t *testing.T) {
		chk := assert.New(t)
		//
		var wg sync.WaitGroup
		var err error
		//
		subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
		eventsC := make(chan interface{}, test.Clients*test.Subscriptions)
		srv := testsuite.TestServer{
			Server: server.Server{
				Events: eventsC,
			},
			AsNetwork: network,
		}
		defer srv.Shutdown()
		//
		ConsumersReadyChannels := []chan struct{}{}
		ConsumersErrorChannels := []chan error{}
		for n := 0; n < test.Clients; n++ { // consumers
			readyC, errorC, err := Consumer(&srv, subscriptions, &wg)
			if !chk.NoError(err) {
				t.FailNow()
			}
			ConsumersReadyChannels = append(ConsumersReadyChannels, readyC)
			ConsumersErrorChannels = append(ConsumersErrorChannels, errorC)
		}
		// Read all consumer read channels; this ensures all subscriptions are active
		// before production begins.
		for _, c := range ConsumersReadyChannels { // wait for all consumers to be ready
			<-c
		}
		//
		// Start the message producer.
		StopProducerC, ProducerErrorC, err := Producer(&srv, subscriptions, &wg)
		if !chk.NoError(err) {
			t.FailNow()
		}
		//
		start, stop := 0, 0
	EventLoop:
		for opaque := range eventsC {
			switch opaque.(type) {
			case events.SubscriptionStart:
				start++
			case events.SubscriptionStop:
				stop++
				if start > 0 && start == stop {
					close(StopProducerC)
					break EventLoop
				}
			}
		}
		wg.Wait()
		for _, c := range ConsumersErrorChannels { // none of the consumers can have error
			for err := range c {
				chk.NoError(err)
			}
		}
		for err := range ProducerErrorC { // producer shouldn't have an error
			chk.NoError(err)
		}
	}
	//
	//
	for _, test := range tests {
		name := fmt.Sprintf("clients=%v subscriptions=%v send=%v", test.Clients, test.Subscriptions, test.Send)
		t.Run("pipe "+name, func(t *testing.T) {
			Fn(test, false, t)
		})
		t.Run("network "+name, func(t *testing.T) {
			Fn(test, true, t)
		})
	}
}

func TestServer_FramesDeliveredIfClientEOFs(t *testing.T) {
	// If a client connects to the server, sends frames, and then disconnects
	// the server should deliver all of the sent frames.
	const Topic string = "/test/server-frames-delivered-if-client-eofs"
	type TestCase struct {
		Producers     int
		Consumers     int
		Subscriptions int
		Send          int
	}
	Consumer := func(gotC chan struct{}, srv *testsuite.TestServer, subscriptions testsuite.Subscriptions, wg *sync.WaitGroup) (ReadyC chan struct{}, ErrorC chan error, err error) {
		ReadyC, ErrorC = make(chan struct{}), make(chan error, 1)
		consumer, err := srv.Consumer(wg)
		if err != nil {
			close(ReadyC)
			close(ErrorC)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, s := range subscriptions {
				consumer.Send <- frames.Subscribe(s.Topic, "", "")
			}
			close(ReadyC)
			//
			connected := true
			for connected {
				select {
				case _, connected = <-consumer.Receive:
					if connected {
						gotC <- struct{}{}
					}
				}
			}
			ErrorC <- <-consumer.Error // should be EOF or nil
			close(ErrorC)
		}()
		return
	}
	Producer := func(beginProductionC chan struct{}, send int, srv *testsuite.TestServer, subscriptions testsuite.Subscriptions, wg *sync.WaitGroup) (ErrorC chan error, err error) {
		ErrorC = make(chan error, 1)
		producer, err := srv.Producer(wg)
		if err != nil {
			close(ErrorC)
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-beginProductionC
			subscriptions.Send(send, producer.Send)
			ErrorC <- producer.Shutdown()
			close(ErrorC)
		}()
		return
	}
	Fn := func(test TestCase, network bool, t *testing.T) {
		chk := assert.New(t)
		//
		var wg sync.WaitGroup
		srv := testsuite.TestServer{
			Server:    server.Server{},
			AsNetwork: network,
		}
		subscriptions := testsuite.NewSubscriptions(Topic, test.Subscriptions)
		//
		ConsumedC := make(chan struct{}) // unbuffered slow consumption slightly, allows messages to "back up" more in server
		ConsumerReadyC := []chan struct{}{}
		ConsumerErrorC := []chan error{}
		for n := 0; n < test.Consumers; n++ {
			readyC, errorC, err := Consumer(ConsumedC, &srv, subscriptions, &wg)
			if !chk.NoError(err) {
				t.FailNow()
			}
			ConsumerReadyC = append(ConsumerReadyC, readyC)
			ConsumerErrorC = append(ConsumerErrorC, errorC)
		}
		for _, c := range ConsumerReadyC {
			for range c {
			}
		}
		//
		BeginProductionC := make(chan struct{})
		ProducerErrorC := []chan error{}
		for n := 0; n < test.Producers; n++ {
			errorC, err := Producer(BeginProductionC, test.Send, &srv, subscriptions, &wg)
			if !chk.NoError(err) {
				t.FailNow()
			}
			ProducerErrorC = append(ProducerErrorC, errorC)
		}
		close(BeginProductionC) // producers wait on this close
		//
		ExpectCount := test.Producers * test.Consumers * test.Subscriptions * test.Send
		GotCount := 0
	ConsumeLoop:
		for {
			select {
			case <-ConsumedC: // every consumed message ticks this channel
				if GotCount = GotCount + 1; GotCount == ExpectCount {
					break ConsumeLoop
				}
			case <-time.After(time.Second):
				t.Error("timeout waiting on consume")
				break ConsumeLoop
			}
		}
		close(ConsumedC) // not strictly necessary but if we close this and get a "panic on send" then the test logic is faulty
		//
		for _, client := range srv.Consumers {
			err := client.Shutdown()
			chk.NoError(err)
		}
		wg.Wait()
		for _, errorC := range ConsumerErrorC {
			for err := range errorC {
				if err != nil {
					chk.ErrorIs(err, io.EOF)
				}
			}
		}
		for _, errorC := range ProducerErrorC {
			for err := range errorC {
				chk.NoError(err)
			}
		}
	}
	tests := []TestCase{
		{Producers: 1, Consumers: 1, Subscriptions: 1, Send: 10},
		{Producers: 2, Consumers: 1, Subscriptions: 1, Send: 10},
		{Producers: 5, Consumers: 1, Subscriptions: 5, Send: 10},
		//
		{Producers: 5, Consumers: 5, Subscriptions: 5, Send: 10},
	}
	for _, test := range tests {
		name := fmt.Sprintf("producers=%v consumers=%v subscriptions=%v send=%v", test.Producers, test.Consumers, test.Subscriptions, test.Send)
		t.Run("pipe "+name, func(t *testing.T) {
			Fn(test, false, t)
		})
		t.Run("network "+name, func(t *testing.T) {
			Fn(test, true, t)
		})
	}
}

func BenchmarkServer(b *testing.B) {
	const Topic string = "/bench/server"
	type Bench struct {
		Producers     int
		Consumers     int
		Subscriptions int
		Send          int
	}
	Consumer := func(num int, bench Bench, srv *testsuite.TestServer, subscriptions testsuite.Subscriptions, readyC chan struct{}, errorC chan error, wg *sync.WaitGroup) error {
		consumer, err := srv.Consumer(wg)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, s := range subscriptions {
				consumer.Send <- frames.Subscribe(s.Topic, "", "")
			}
			// A round trip message to ensure we're subscribed
			rt := fmt.Sprintf("bench/server/consumer-ready-%v", num)
			consumer.Send <- frames.Subscribe(rt, "", "")
			consumer.Send <- frames.Send(rt, nil)
			<-consumer.Receive
			//
			readyC <- struct{}{}
			got := 0
			expect := bench.Producers * bench.Subscriptions * bench.Send
		ConsumeLoop:
			for {
				select {
				case <-consumer.Receive:
					if got = got + 1; got == expect {
						break ConsumeLoop
					}
				case err := <-consumer.Error:
					errorC <- err
					return
				}
			}
			errorC <- consumer.Shutdown()
		}()
		return nil
	}
	Producer := func(bench Bench, srv *testsuite.TestServer, subscriptions testsuite.Subscriptions, produceC chan struct{}, errorC chan error, wg *sync.WaitGroup) error {
		producer, err := srv.Producer(wg)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-produceC // wait till close
			subscriptions.Send(bench.Send, producer.Send)
			errorC <- producer.Shutdown()
		}()
		return nil
	}
	Fn := func(bench Bench, network bool, b *testing.B) {
		var wg sync.WaitGroup
		srv := testsuite.TestServer{
			Server:    server.Server{},
			AsNetwork: network,
		}
		subscriptions := testsuite.NewSubscriptions(Topic, bench.Subscriptions)
		//
		ConsumerReadyC := make(chan struct{}, bench.Consumers)
		ConsumerErrorC := make(chan error, bench.Consumers)
		for n := 0; n < bench.Consumers; n++ {
			err := Consumer(n, bench, &srv, subscriptions, ConsumerReadyC, ConsumerErrorC, &wg)
			if err != nil {
				b.Fatalf(err.Error())
			}
		}
		for n := bench.Consumers; n != 0; n-- {
			<-ConsumerReadyC
		}
		close(ConsumerReadyC) // not strictly necessary but if panics our benchmark logic is wrong
		//
		BeginProductionC := make(chan struct{})
		ProducerErrorC := make(chan error, bench.Producers)
		for n := 0; n < bench.Producers; n++ {
			err := Producer(bench, &srv, subscriptions, BeginProductionC, ProducerErrorC, &wg)
			if err != nil {
				b.Fatalf(err.Error())
			}
		}
		close(BeginProductionC)
		//
		for n := bench.Consumers; n != 0; n-- {
			err := <-ConsumerErrorC
			if err != nil {
				b.Fatalf(err.Error())
			}
		}
		for n := bench.Producers; n != 0; n-- {
			err := <-ProducerErrorC
			if err != nil {
				b.Fatalf(err.Error())
			}
		}
		//
		if err := srv.Shutdown(); err != nil {
			b.Fatalf(err.Error())
		}
		wg.Wait()
	}
	benches := []Bench{
		{Producers: 1, Consumers: 1, Subscriptions: 1, Send: 10},
		{Producers: 1, Consumers: 1, Subscriptions: 1, Send: 100},
		//
		{Producers: 10, Consumers: 1, Subscriptions: 1, Send: 10},
		{Producers: 1, Consumers: 10, Subscriptions: 1, Send: 10},
		//
		{Producers: 10, Consumers: 1, Subscriptions: 10, Send: 10},
		{Producers: 1, Consumers: 10, Subscriptions: 10, Send: 10},
	}
	for _, bench := range benches {
		name := fmt.Sprintf("producers=%v consumers=%v subscriptions=%v send=%v", bench.Producers, bench.Consumers, bench.Subscriptions, bench.Send)
		b.Run("pipe "+name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				Fn(bench, false, b)
			}
		})
		b.Run("network "+name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				Fn(bench, false, b)
			}
		})
	}
}
