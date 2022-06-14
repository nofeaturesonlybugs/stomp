package stomp

// Command is a STOMP command and often the first line in a STOMP frame.
type Command string

const (
	CommandAbort       Command = "ABORT"
	CommandAck                 = "ACK"
	CommandBegin               = "BEGIN"
	CommandCommit              = "COMMIT"
	CommandConnect             = "CONNECT"
	CommandConnected           = "CONNECTED"
	CommandDisconnect          = "DISCONNECT"
	CommandError               = "ERROR"
	CommandMessage             = "MESSAGE"
	CommandReceipt             = "RECEIPT"
	CommandSend                = "SEND"
	CommandStomp               = "STOMP"
	CommandSubscribe           = "SUBSCRIBE"
	CommandUnsubscribe         = "UNSUBSCRIBE"
)
