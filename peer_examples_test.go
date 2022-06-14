package stomp_test

import (
	"fmt"
	"sync"

	"github.com/nofeaturesonlybugs/stomp"
)

func ExamplePipe_rW() {
	// After calling stomp.Pipe this example uses the R and W struct fields directly:
	// local.W is piped to remote.R
	// remote.W is piped to local.R
	local, remote := stomp.Pipe()

	// The frames to send.
	localFrame := stomp.Frame{
		Command: stomp.CommandMessage,
		Headers: nil,
		Body:    []byte("Hello from local!"),
	}
	remoteFrame := stomp.Frame{
		Command: stomp.CommandMessage,
		Headers: nil,
		Body:    []byte("Hello from remote!"),
	}

	var wg sync.WaitGroup
	var lmsg, rmsg string
	wg.Add(2)
	// Writing to local.W or remote.W will block until readers are ready so set readers up now.
	go func() {
		defer wg.Done()
		p := stomp.NewParser(remote.R)
		f, _ := p.Frame() // ignoring error
		rmsg = string(f.Body)
	}()
	go func() {
		defer wg.Done()
		p := stomp.NewParser(local.R)
		f, _ := p.Frame() // ignoring error
		lmsg = string(f.Body)
	}()

	_, err := localFrame.WriteTo(local.W) // Write local frame to local.W which pipes to remote.R
	if err != nil {
		fmt.Println("local write", err)
		return
	}
	_, err = remoteFrame.WriteTo(remote.W) // Write remote frame to remote.W which pipes to local.R
	if err != nil {
		fmt.Println("remote write", err)
		return
	}

	wg.Wait()
	fmt.Println(rmsg)
	fmt.Println(lmsg)

	// Output: Hello from local!
	// Hello from remote!
}

func ExamplePipe_channels() {
	// This example calls Start on the peers returned from Pipe, which starts goroutines and allows
	// channel based communication.
	local, remote := stomp.Pipe()
	local.Start(nil)  // The waitgroup argument is optional
	remote.Start(nil) // The waitgroup argument is optional

	// The frames to send.
	localFrame := stomp.Frame{
		Command: stomp.CommandMessage,
		Headers: nil,
		Body:    []byte("Hello from local!"),
	}
	remoteFrame := stomp.Frame{
		Command: stomp.CommandMessage,
		Headers: nil,
		Body:    []byte("Hello from remote!"),
	}

	var wg sync.WaitGroup
	var lmsg, rmsg string
	wg.Add(2)
	go func() {
		defer wg.Done()
		f := <-remote.Receive
		rmsg = string(f.Body)
	}()
	go func() {
		defer wg.Done()
		f := <-local.Receive
		lmsg = string(f.Body)
	}()

	local.Send <- localFrame
	remote.Send <- remoteFrame

	wg.Wait()
	fmt.Println(rmsg)
	fmt.Println(lmsg)

	if err := local.Shutdown(); err != nil {
		fmt.Println("shutdown local", err)
	}
	if err := remote.Shutdown(); err != nil {
		fmt.Println("shutdown remote", err)
	}

	// Output: Hello from local!
	// Hello from remote!
}
