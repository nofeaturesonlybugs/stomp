package stomp

import "sort"

const (
	HeaderAck           = "ack"
	HeaderContentLength = "content-length"
	HeaderDestination   = "destination"
	HeaderID            = "id"
	HeaderLogin         = "login"
	HeaderMessage       = "message"
	HeaderMessageID     = "message-id"
	HeaderPasscode      = "passcode"
	HeaderReplyTo       = "reply-to"
	HeaderSession       = "session"
)

// Headers are the frame headers.
type Headers map[string]string

// SortedKeys returns a slice of the Headers sorted.
func (h Headers) SortedKeys() []string {
	var keys []string
	if n := len(h); n > 0 {
		keys = make([]string, 0, n)
		for k := range h {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	return keys
}
