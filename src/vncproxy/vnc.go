package vncproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/phayes/freeport"
)

// VncConfiguration is a structure holding configurations for the local environment
type VncConfiguration struct {
	XserverCmdTemplate   string // X server command template
	VncServerCmdTemplate string // VNC server command template
	DisplayFd            int    // File descriptor used for passing display number back from X Server
}

// VncSession manages VNC and related X server instances
type VncSession struct {
	Config      VncConfiguration      // The configuration of the
	display     string                // The X display for the session
	localPort   int                   // The local port of the associated vnc server
	localPortV6 int                   // The local port for IP V6 communication
	authSocket  string                // Tbe auth socket for the X server
	xserver     *exec.Cmd             // Pointer to the X server shell command
	vncserver   *exec.Cmd             // Poiner to the VNC server shell command
	events      chan VncSessionEvent  // A channel to broadcast state changes of the VncSession
	Callback    func(VncSessionEvent) // Callback function to react to state changes
}

// VncSessionEvent is used to send state-change events
type VncSessionEvent int

// Pre-defined VncSession state-change events
const (
	VncSessionXServerStarted   VncSessionEvent = iota
	VncSessionXServerStopped   VncSessionEvent = iota
	VncSessionVncServerStarted VncSessionEvent = iota
	VncSessionVncServerStopped VncSessionEvent = iota
	VncSessionEventListenerSet VncSessionEvent = iota
)

// NewVncConfiguration creates a default VNC configuration
func NewVncConfiguration() VncConfiguration {

	return VncConfiguration{
		XserverCmdTemplate:   "/usr/bin/X -displayfd {{.Config.DisplayFd}} -auth {{.AuthSocket}}",
		VncServerCmdTemplate: "/usr/bin/x11vnc -xkb -noxrecord -noxfixes -noxdamage -rfbport {{.VncPort}} -rfbportv6 {{.VncPortV6}} -display :{{.Display}} -auth {{.AuthSocket}} -ncache 10 -o /var/log/vnc-{{.Display}}",
		DisplayFd:            6,
	}

}

// NewVncSession creates a new VNC session by instancing
// associated X11 and VNC servers
func NewVncSession() *VncSession {

	return &VncSession{
		Config:     NewVncConfiguration(),
		xserver:    nil,
		vncserver:  nil,
		display:    "",
		localPort:  0,
		authSocket: "",
		events:     make(chan VncSessionEvent, 100),
		Callback:   nil,
	}

}

// ****************************************************************************
// CONSTRUSTORS
// ****************************************************************************

// Start starts the associated X and VNC server
func (s *VncSession) Start() error {

	// Start X Server
	if err := s.createAndStartXServer(); err != nil {
		return err
	}

	<-time.After(time.Second)

	// Start VNC Server
	if err := s.createAndStartVncServer(); err != nil {
		return err
	}

	// Initiate callback routine
	go func() {
		for {
			ev := <-s.events
			if s.Callback != nil {
				s.Callback(ev)
			}
		}
	}()

	return nil
}

// Close closes the VNC session. It stops the associated X and VNC server and frees other resources
func (s *VncSession) Close() {

	// Stop the VNC server
	if s.vncserver != nil {
		if err := s.vncserver.Process.Kill(); err != nil {
			fmt.Println("Could not kill VNC server: " + err.Error())
		}
	}

	// Stop the X server
	if s.xserver != nil {
		if err := s.xserver.Process.Kill(); err != nil {
			fmt.Println("Could not kill X server: " + err.Error())
		}
	}

	// Remove the authSocket
	if err := os.Remove(s.authSocket); err != nil {
		fmt.Println("Could not remove auth socket: " + err.Error())
	}
}

// ****************************************************************************
// GETTERS
// ****************************************************************************

// Display returns the X server display number or an empty string if no X
// Server is running
func (s *VncSession) Display() string {
	return s.display
}

// AuthSocket returns the X server authentication socket or an empty string if
// no X server is running
func (s *VncSession) AuthSocket() string {
	return s.authSocket
}

// VncPort returns the port at which the VNC server is listening
func (s *VncSession) VncPort() int {
	return s.localPort
}

// VncPortV6 returns the port at which the VNC server is listening for IP V6 traffic
func (s *VncSession) VncPortV6() int {
	return s.localPortV6
}

// ****************************************************************************
// XSERVER ROUTINES
// ****************************************************************************

// findFreeDisplay returns a free X display to use
func (s *VncSession) createAndStartXServer() error {

	// Create authentication socket
	auth := s.generateAuthSocketFile()
	defer auth.Close()
	s.authSocket = auth.Name()

	// Start X server
	s.xserver = exec.Command("/bin/sh", "-c", s.getXServerCmd())
	if err := s.xserver.Start(); err != nil {
		fmt.Println("Error starting X server: " + err.Error())
		return err
	}
	s.events <- VncSessionXServerStarted

	// Listen for termination of the X server and broadcast
	go func() {
		s.xserver.Wait()
		fmt.Println("X server stopped")
		s.events <- VncSessionXServerStopped
	}()

	// Obtain display for X server
	v, err := s.readDisplayFromFd()
	if err != nil {
		fmt.Println(err.Error())
		s.Close()
		return err
	}
	s.display = v

	// Communicate success
	s.events <- VncSessionXServerStarted
	fmt.Println("X server started at display :" + s.display)
	return nil
}

func (s *VncSession) generateAuthSocketFile() *os.File {

	fn, err := ioutil.TempFile("/tmp", ".serverauth-")
	if err != nil {
		panic(err)
	}
	return fn

}

// readDisplayFromFd find display ID of the instantiated X server
// Note: this relies on the -displayfd feature
// Because the X server does not write the display ID immediately , we use
// an asynchronous approach with timeout
// (see: https://stackoverflow.com/questions/2520704/find-a-free-x11-display-number)
func (s *VncSession) readDisplayFromFd() (string, error) {

	dch := make(chan string, 2)

	go func() {
		for {
			if fd, err := ioutil.ReadFile(s.AuthSocket() + "-fd"); err == nil {
				str := strings.TrimSpace(string(fd))

				// Check that content is number
				if _, err2 := strconv.Atoi(str); err2 != nil {
					continue
				}

				dch <- str // Communicate success
				return
			}

			time.Sleep(1 * time.Millisecond)
		}
	}()

	select {
	case res := <-dch:
		return res, nil
	case <-time.After(5 * time.Second):
		return "", errors.New("X server did not communicate display")
	}

}

func (s *VncSession) getXServerCmd() string {

	tmpl, err := template.New("X").Parse(
		"exec {{.Config.DisplayFd}}<> {{.AuthSocket}}-fd &&" +
			s.Config.XserverCmdTemplate +
			" && exec {{.Config.DisplayFd}}>&-")
	if err != nil {
		panic(err)
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, s); err != nil {
		panic(err)
	}
	return buffer.String()

}

// ****************************************************************************
// VNC SERVER ROUTINES
// ****************************************************************************

// findFreeDcreateAndStartVncServer creates the VNC server
func (s *VncSession) createAndStartVncServer() error {

	// Check that X server authority socket is configured
	if s.AuthSocket() == "" {
		return errors.New("X Server authority socket not set")
	}

	// Check that X server has provided the display
	if s.Display() == "" {
		return errors.New("X Server display not set")
	}

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

	// Start VNC server
	s.vncserver = exec.Command("/bin/sh", "-c", s.getVncServerCmd())
	if err := s.vncserver.Start(); err != nil {
		fmt.Println("Error starting VNC server: " + err.Error())
		return err
	}
	fmt.Println("Executing: " + s.getVncServerCmd())
	fmt.Println("VNC server will listen on port " + strconv.Itoa(s.VncPort()))
	s.events <- VncSessionVncServerStarted

	// Listen for termination of the X server and broadcast
	go func() {
		s.vncserver.Wait()
		fmt.Println("VNC server stopped")
		s.events <- VncSessionVncServerStopped
	}()

	return nil

}

func (s *VncSession) getVncServerCmd() string {

	tmpl, err := template.New("X").Parse(s.Config.VncServerCmdTemplate)
	if err != nil {
		panic(err)
	}
	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, s); err != nil {
		panic(err)
	}
	return buffer.String()
}
