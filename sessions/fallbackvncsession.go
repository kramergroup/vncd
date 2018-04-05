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
	"syscall"
	"text/template"
	"time"

	"github.com/phayes/freeport"
)

/*
  This is a fallback implementation of the VncSession interface. It is fully
  self-contained and does not rely on external scripts, etc. It should work on
  most system with X11 and x11vnc installed.
*/

// FallbackVncConfigureation is a structure holding configurations for the local environment
type FallbackVncConfigureation struct {
	StartVncScript       string // Shell script to start VNC server
	XserverCmdTemplate   string // X server command template
	VncServerCmdTemplate string // VNC server command template
	DisplayFd            int    // File descriptor used for passing display number back from X Server
}

// FallbackVncSession manages VNC and related X server instances
type FallbackVncSession struct {
	Config      FallbackVncConfigureation // The configuration of the
	display     string                    // The X display for the session
	localPort   int                       // The local port of the associated vnc server
	localPortV6 int                       // The local port for IP V6 communication
	authSocket  string                    // Tbe auth socket for the X server
	xserver     *exec.Cmd                 // Pointer to the X server shell command
	vncserver   *exec.Cmd                 // Poiner to the VNC server shell command
	events      chan VncSessionEvent      // A channel to broadcast state changes of the VncSession
	Callback    func(VncSessionEvent)     // Callback function to react to state changes
}

// ****************************************************************************
// CONSTRUSTORS
// ****************************************************************************

// NewFallbackVncConfiguration creates a default VNC configuration
func NewFallbackVncConfiguration() FallbackVncConfigureation {

	return FallbackVncConfigureation{
		StartVncScript:       "/etc/vncd/startvnc.sh",
		XserverCmdTemplate:   "/usr/bin/X -displayfd {{.Config.DisplayFd}} -auth {{.AuthSocket}}",
		VncServerCmdTemplate: "/usr/bin/x11vnc -xkb -noxrecord -noxfixes -noxdamage -rfbport {{.VncPort}} -rfbportv6 {{.VncPortV6}} -display :{{.Display}} -auth {{.AuthSocket}} -ncache 10 -o /var/log/vnc-{{.Display}}",
		DisplayFd:            6,
	}

}

// NewFallbackVncSession creates a new VNC session by instancing
// associated X11 and VNC servers
func NewFallbackVncSession() *FallbackVncSession {

	return &FallbackVncSession{
		Config:     NewFallbackVncConfiguration(),
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
// VncSession Interface
// ****************************************************************************

// Start starts the associated X and VNC server
func (s *FallbackVncSession) Start() error {

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
func (s *FallbackVncSession) Close() {

	// Stop the VNC server
	if s.vncserver != nil {
		if err := syscall.Kill(-s.vncserver.Process.Pid, syscall.SIGKILL); err != nil {
			fmt.Println("Could not kill VNC server: " + err.Error())
		}
	}

	// Stop the X server
	if s.xserver != nil {
		if err := syscall.Kill(-s.xserver.Process.Pid, syscall.SIGKILL); err != nil {
			fmt.Println("Could not kill X server: " + err.Error())
		}
	}

	// Remove the authSocket
	if err := os.Remove(s.authSocket); err != nil {
		fmt.Println("Could not remove auth socket: " + err.Error())
	}
}

// SetCallback sets a callback method that is triggered by state changes
func (s *FallbackVncSession) SetCallback(cb func(VncSessionEvent)) {
	s.Callback = cb
}

// VncPort returns the port at which the VNC server is listening
func (s *FallbackVncSession) VncPort() int {
	return s.localPort
}

// VncPortV6 returns the port at which the VNC server is listening for IP V6 traffic
func (s *FallbackVncSession) VncPortV6() int {
	return s.localPortV6
}

// ****************************************************************************
// GETTERS
// ****************************************************************************

// Display returns the X server display number or an empty string if no X
// Server is running
func (s *FallbackVncSession) Display() string {
	return s.display
}

// AuthSocket returns the X server authentication socket or an empty string if
// no X server is running
func (s *FallbackVncSession) AuthSocket() string {
	return s.authSocket
}

// ****************************************************************************
// XSERVER ROUTINES
// ****************************************************************************

// findFreeDisplay returns a free X display to use
func (s *FallbackVncSession) createAndStartXServer() error {

	// Create authentication socket
	auth := s.generateAuthSocketFile()
	defer auth.Close()
	s.authSocket = auth.Name()

	// Start X server
	s.xserver = exec.Command("/bin/sh", "-c", s.getXServerCmd())
	s.xserver.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := s.xserver.Start(); err != nil {
		fmt.Println("Error starting X server: " + err.Error())
		return err
	}

	// Obtain display for X server
	v, err := s.readDisplayFromFd()
	if err != nil {
		fmt.Println(err.Error())
		s.Close()
		return err
	}
	s.display = v

	// Communicate success
	fmt.Println("X server started at display :" + s.display)
	return nil
}

func (s *FallbackVncSession) generateAuthSocketFile() *os.File {

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
func (s *FallbackVncSession) readDisplayFromFd() (string, error) {

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

func (s *FallbackVncSession) getXServerCmd() string {

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
func (s *FallbackVncSession) createAndStartVncServer() error {

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
	s.vncserver.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

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

func (s *FallbackVncSession) getVncServerCmd() string {

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
