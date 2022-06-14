package stomp

import "errors"

var (
	// ErrDuplicateSubscription occurs when a client subscribes to the same <destination,id> tuple.
	ErrDuplicateSubscription = errors.New("stomp: duplicate subscription")

	// ErrFrame occurs when the reader returns any error during parsing of a STOMP frame.
	ErrFrame = errors.New("stomp: invalid frame")

	// ErrMissingHeader occurs when a frame is missing a required header.
	ErrMissingHeader = errors.New("stomp: missing required header")
)
