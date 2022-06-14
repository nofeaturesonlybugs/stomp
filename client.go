package stomp

// import (
// 	"crypto/tls"
// 	"net"
// 	"sync"
// )

// // Client is a STOMP client.
// //
// // Client should not be copied once connected.
// type Client struct {
// 	// Addr specifies the TCP address for the server as "host:port".
// 	Addr string

// 	// Dialer specifies an optional Dialer configuration.
// 	Dialer *net.Dialer

// 	// TLSConfig specifies an optional TLS configuration.
// 	TLSConfig *tls.Config

// 	conn net.Conn
// 	wg   sync.WaitGroup
// }

// // Connect connects to the server specified in Client.Addr.
// func (c *Client) Connect() error {
// 	var conn net.Conn
// 	var err error
// 	//
// 	if c.TLSConfig != nil {
// 		if c.Dialer != nil {
// 			dialer := tls.Dialer{
// 				NetDialer: c.Dialer,
// 				Config:    c.TLSConfig,
// 			}
// 			if conn, err = dialer.Dial("tcp", c.Addr); err != nil {
// 				return err
// 			}
// 		} else {
// 			if conn, err = tls.Dial("tcp", c.Addr, c.TLSConfig); err != nil {
// 				return err
// 			}
// 		}
// 	} else {
// 		if c.Dialer != nil {
// 			if conn, err = c.Dialer.Dial("tcp", c.Addr); err != nil {
// 				return err
// 			}
// 		} else {
// 			if conn, err = net.Dial("tcp", c.Addr); err != nil {
// 				return err
// 			}
// 		}
// 	}
// 	//
// 	// TODO STOMP 1.0 send CONNECT frame
// 	f := Frame{
// 		Command: "CONNECT",
// 		Headers: Headers{
// 			"login":    "username", // TODO+NB
// 			"passcode": "hunter12", // TODO+NB
// 			// "accept-version":"1.0,1.1,2.0", // TODO+NB STOMP 1.1
// 			// "heart-beat":"0,0", // TODO+NB STOMP 1.1
// 		},
// 	}
// 	if _, err = f.WriteTo(conn); err != nil {
// 		conn.Close()
// 		return err
// 	}
// 	//
// 	c.conn = conn
// 	//
// 	return nil
// }
