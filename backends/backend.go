package backends

import (
	"net"
)

/******************************************************************************
  Backend interface
 ******************************************************************************/

// Backend is the interface that is implemented by all handling backends
type Backend interface {
	GetTarget() *net.TCPAddr // GetTarget returns the listening IP address of the backend
	Terminate()              // Terminate the backend
}
