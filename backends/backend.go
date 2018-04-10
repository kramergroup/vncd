package backends

import "net"

/*
 ------------------------------------------------------------------------------
  Backend interface
 ------------------------------------------------------------------------------
*/

// Backend is the interface that is implemented by all handling backends
type Backend interface {
	GetTarget() *net.TCPAddr // GetTarget returns the listening IP address of the backend
	Terminate()              // Terminate the backend
}

// GetBackendForConnection returns a handling backend for the incomming connection
func GetBackendForConnection(conn net.Conn, config string) (Backend, error) {
	return nil, nil
}

/*
 ------------------------------------------------------------------------------
  Implementation
 ------------------------------------------------------------------------------
*/
