package frames

import (
	"bytes"

	"github.com/nofeaturesonlybugs/stomp"
)

// Empty is an empty STOMP frame and is provided as a convenience.
var Empty stomp.Frame

// Connect creates a CONNECT frame using the given credentials.
func Connect(login, passcode string) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandConnect,
		Headers: stomp.Headers{
			stomp.HeaderLogin:    login,
			stomp.HeaderPasscode: passcode,
		},
	}
	return f
}

// Connected creates a CONNECTED frame.
func Connected(session string) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandConnected,
		Headers: stomp.Headers{
			stomp.HeaderSession: session,
		},
	}
	return f
}

// Error creates an ERROR frame from an existing frame.
//
// message becomes the message header in the returned frame which should
// be a short description of the error.
//
// If present body becomes the leading portion of the frame body.
//
// If frame is a non-empty frame then it will be partially inserted into the returned
// frame's body to allow for contextual information about a frame causing the error.
func Error(message string, body string, frame stomp.Frame) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandError,
		Headers: stomp.Headers{
			stomp.HeaderMessage: message,
		},
	}
	buf := &bytes.Buffer{}
	if !frame.Empty() {
		buf.WriteString("The frame\n----\n")
		_, _ = frame.WriteTo(buf) // TODO Error here?  There's no feasible reason this should fail.
		buf.WriteString("\n----\n")
	}
	if body != "" {
		buf.WriteString(body)
		buf.WriteString("\n")
	}
	if buf.Len() > 0 {
		f.Body = buf.Bytes()
	}
	return f
}

// Send creates a SEND frame.
//
// dest is required by STOMP protocol but not enforced by this function.
func Send(dest string, body []byte) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandSend,
		Headers: stomp.Headers{
			stomp.HeaderDestination: dest,
		},
		Body: body,
	}
	return f
}

// SendString creates a SEND frame from a string message body.
//
// dest is required by STOMP protocol but not enforced by this function.
func SendString(dest string, body string) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandSend,
		Headers: stomp.Headers{
			stomp.HeaderDestination: dest,
		},
		Body: []byte(body),
	}
	return f
}

// Subscribe creates a SUBSCRIBE frame.
//
// dest is required by STOMP protocol but not enforced by this function.
// ack and id are optional and should be set according to STOMP protocol.
func Subscribe(dest, ack, id string) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandSubscribe,
		Headers: stomp.Headers{
			stomp.HeaderDestination: dest,
		},
	}
	if ack != "" {
		f.Headers[stomp.HeaderAck] = ack
	}
	if id != "" {
		f.Headers[stomp.HeaderID] = id
	}
	return f
}

// Unsubscribe creates an UNSUBSCRIBE frame.
//
// One of dest or id is required and they are mutually exclusive; however this is not
// enforced by this function.
func Unsubscribe(dest, id string) stomp.Frame {
	f := stomp.Frame{
		Command: stomp.CommandUnsubscribe,
		Headers: stomp.Headers{},
	}
	if dest != "" {
		f.Headers[stomp.HeaderDestination] = dest
	}
	if id != "" {
		f.Headers[stomp.HeaderID] = id
	}
	return f
}
