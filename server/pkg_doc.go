// Package server contains a STOMP server implementation.
//
// TODO
//
// Authentication.  Currently all peers are accepted without authentication.
//
// Destination authentication.  Currently peers can subscribe to any destination.
//
// Queues.  Currently all destinations are pure broadcasts; allow destinations
// to become round-robin work queues.
package server
