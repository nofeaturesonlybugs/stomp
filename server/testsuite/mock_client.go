package testsuite

import (
	"errors"
	"fmt"
	"time"

	"github.com/nofeaturesonlybugs/stomp"
	"github.com/nofeaturesonlybugs/stomp/frames"
)

const (
	TestLogin    = "testlogin"
	TestPasscode = "testpasscode"
)

// MockClient is a mock client peer for interacting with a stomp/server.Server.
type MockClient struct {
	stomp.Peer
}

// Connect sends a CONNECT frame and verifies the session value received.
func (client MockClient) Connect(sessionID string) error {
	select {
	case client.Send <- frames.Connect(TestLogin, TestPasscode):
	default:
		return errors.New("unable to send CONNECT frame")
	}
	select {
	case frame := <-client.Receive:
		if frame.Command != stomp.CommandConnected {
			return fmt.Errorf("expected %v frame; got %v", stomp.CommandConnected, frame.Command)
		} else if id := frame.Headers[stomp.HeaderSession]; id != sessionID {
			return fmt.Errorf("expected %v session; got %v", sessionID, id)
		}
	case err := <-client.Error:
		return err
	case <-time.After(100 * time.Millisecond):
		return errors.New("timeout during CONNECT")
	}
	return nil
}

// Frame sends a single frame.
func (client MockClient) Frame(frame stomp.Frame) error {
	select {
	case client.Send <- frame:
	default:
		return errors.New("unable to send frame")
	}
	return nil
}
