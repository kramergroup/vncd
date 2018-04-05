package vncd

// VncSession encapuslates a VNC Server instance
type VncSession interface {
	Start() error                      // Start the VNC server
	Close()                            // Stop the VNC server
	SetCallback(func(VncSessionEvent)) // Set callback function
	VncPort() int                      // return the TCP V4 port of the VNC server
	VncPortV6() int                    // return the TCP V6 port of the VNC server
}

// VncSessionEvent is used to send state-change events
type VncSessionEvent int

// Pre-defined VncSession state-change events
const (
	VncSessionVncServerStarted VncSessionEvent = iota
	VncSessionVncServerStopped VncSessionEvent = iota
	VncSessionEventListenerSet VncSessionEvent = iota
)

// ****************************************************************************
// CONSTRUSTORS
// ****************************************************************************

// NewVncSession creates a new VncSession. The method first tries to instantiate
// a DefaultVncSession and if that is unsuccessful it falls back to a reference
// implementation that should work on most systems.
func NewVncSession() VncSession {

	s, err := NewDefaultVncSession()
	if err == nil {
		return s
	}

	return NewFallbackVncSession()
}
