package vncd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/phayes/freeport"
)

const (
	// DefaultStartVncShellScript is the default location of the startvnc shell script
	DefaultStartVncShellScript = "/etc/vncd/startvnc.sh"
)

// DefaultVncSession is a VncSession implementation that relies on an
// external shell script to start the VNC server.
type DefaultVncSession struct {
	shellScript string
	bootstrap   string
	localPort   int
	localPortV6 int
	vncserver   *exec.Cmd
	callback    func(VncSessionEvent) // Callback function for state changes
}

// ****************************************************************************
// CONSTRUSTORS
// ****************************************************************************

// NewDefaultVncSessionWithScripts creates a DefaultVncSession with custom
// external shell script
func NewDefaultVncSessionWithScripts(shellScript string, bootstrap string) (*DefaultVncSession, error) {

	s := &DefaultVncSession{
		shellScript: shellScript,
		vncserver:   nil,
		callback:    func(e VncSessionEvent) {},
	}

	// Check that script file exists
	if !s.scriptFileExists() {
		return s, errors.New("Shell script not found")
	}

	// All good - return working struct
	return s, nil
}

// NewDefaultVncSession creates a DefaultVncSession using a startvnc script
// in a default location
func NewDefaultVncSession() (*DefaultVncSession, error) {
	return NewDefaultVncSessionWithScripts(DefaultStartVncShellScript, "")
}

// ****************************************************************************
// VncSession Interface
// ****************************************************************************

// Start calls the shell script to instantiate a VNC server
func (s *DefaultVncSession) Start() error {

	// Start VNC Server
	if err := s.createAndStartVncServer(); err != nil {
		return err
	}

	return nil
}

// Close closes the VNC session. It stops the associated VNC server and frees other resources
func (s *DefaultVncSession) Close() {

	// Stop the VNC server
	if s.vncserver != nil {
		if err := syscall.Kill(-s.vncserver.Process.Pid, syscall.SIGKILL); err != nil {
			fmt.Println("Could not kill VNC server: " + err.Error())
		}
	}

}

// VncPort returns the port at which the VNC server is listening
func (s *DefaultVncSession) VncPort() int {
	return s.localPort
}

// VncPortV6 returns the port at which the VNC server is listening for IP V6 traffic
func (s *DefaultVncSession) VncPortV6() int {
	return s.localPortV6
}

// SetCallback sets a callback method that is triggered by state changes
func (s *DefaultVncSession) SetCallback(cb func(VncSessionEvent)) {
	s.callback = cb
}

// ****************************************************************************
// Implementation methods
// ****************************************************************************

func (s *DefaultVncSession) createAndStartVncServer() error {

	// Find a free port to use for communication
	// TODO: This will enable direct communication from the outside. Maybe better to use sockets
	{
		port, err := freeport.GetFreePort()
		if err != nil {
			return err
		}
		s.localPort = port
	}

	// Find a free port to use for communication using IP V6
	// There is a bug in libvncserver that requires configuring a free port for V6
	// even if it is not used
	// https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=735648
	{
		port, err := freeport.GetFreePort()
		if err != nil {
			return err
		}
		s.localPortV6 = port
	}

	// Call shell script
	s.vncserver = exec.Command(
		s.shellScript,
		strconv.Itoa(s.localPort),
		strconv.Itoa(s.localPortV6))

	s.vncserver.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := s.vncserver.Start(); err != nil {
		fmt.Println("Error starting VNC server: " + err.Error())
		return err
	}

	fmt.Println("VNC server will listen on port " + strconv.Itoa(s.VncPort()))
	go s.callback(VncSessionVncServerStarted)

	// Listen for termination of the X server and broadcast
	go func() {
		s.vncserver.Wait()
		fmt.Println("VNC server on port " + strconv.Itoa(s.VncPort()) + " stopped")
		s.callback(VncSessionVncServerStopped)
	}()

	return nil

}

// ****************************************************************************
// Helper methods
// ****************************************************************************
func (s *DefaultVncSession) scriptFileExists() bool {
	_, err := os.Stat(s.shellScript)
	return !os.IsNotExist(err)
}
