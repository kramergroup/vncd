package vncd

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/kramergroup/vncd/backends"
)

// Server is a TCP server that takes an incoming request and sends it to another
// server, proxying the response back to the client.
type Server struct {
	// Target address
	Target *net.TCPAddr

	// Local address
	Addr *net.TCPAddr

	// Director must be a function which modifies the request into a new request
	// to be sent. Its response is then copied back to the client unmodified.
	Director func(b *[]byte)

	// If config is not nil, the proxy connects to the target address and then
	// initiates a TLS handshake.
	Config *tls.Config

	// Timeout is the duration the proxy is staying alive without activity from
	// both client and target. Also, if a pipe is closed, the proxy waits 'timeout'
	// seconds before closing the other one. By default timeout is 60 seconds.
	Timeout time.Duration

	// Creator creates a new Backend for connection requests
	BackendFactory func() (backends.Backend, error)
}

// NewServer created a new proxy which sends all packet to target. The function dir
// intercept and can change the packet before sending it to the target.
func NewServer(dir func(*[]byte), factory func() (backends.Backend, error), config *tls.Config) (*Server, error) {

	p := &Server{
		Director:       dir,
		Config:         config,
		BackendFactory: factory,
	}

	var err error
	if factory == nil {
		err = errors.New("Backend factory method must not be nil")
	}
	return p, err
}

// ListenAndServe listens on the TCP network address laddr and then handle packets
// on incoming connections.
func (p *Server) ListenAndServe(laddr *net.TCPAddr) {
	p.Addr = laddr

	var listener net.Listener
	listener, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	p.serve(listener)
}

// ListenAndServeTLS acts identically to ListenAndServe, except that it uses TLS
// protocol. Additionally, files containing a certificate and matching private key
// for the server must be provided.
func (p *Server) ListenAndServeTLS(laddr *net.TCPAddr, certFile, keyFile string) {
	p.Addr = laddr

	var listener net.Listener
	cer, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		fmt.Println(err)
		return
	}
	config := &tls.Config{Certificates: []tls.Certificate{cer}}
	listener, err = tls.Listen("tcp", laddr.String(), config)
	if err != nil {
		fmt.Println(err)
		return
	}

	p.serve(listener)
}

func (p *Server) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}

		go p.handleConn(conn)
	}
}

// handleConn handles connection.
func (p *Server) handleConn(conn net.Conn) {
	fmt.Println("Incomming connection from " + p.Addr.String())

	// Initiate the backend
	backendCreatedCh := make(chan bool)
	var backend backends.Backend
	go func() {
		var err error
		backend, err = p.BackendFactory()
		if err != nil {
			fmt.Println(err)
		}
		backendCreatedCh <- (err == nil)
	}()

	select {
	case <-time.After(30 * time.Second):
		fmt.Println("Timeout obtaining backend.")
		conn.Close()
		return
	case ok := <-backendCreatedCh:
		if !ok {
			fmt.Println("Failed to obtain backend.")
			conn.Close()
			return
		}
	}

	// Set the proxy Target to the backend
	p.Target = backend.GetTarget()

	// connects to VNC server - try for 5 seconds to give time for VNC to come up
	var rconn net.Conn
	var establishRemoteConn = true
	remoteConnEstablishedCh := make(chan bool)
	go func() {
		var err error
		for establishRemoteConn {
			if p.Config == nil {
				rconn, err = net.Dial("tcp", p.Target.String())
				fmt.Println(err)
				establishRemoteConn = (err != nil)
			} else {
				rconn, err = tls.Dial("tcp", p.Target.String(), p.Config)
				establishRemoteConn = (err != nil)
			}
		}
		remoteConnEstablishedCh <- (err == nil)
	}()

	select {
	case <-time.After(30 * time.Second):
		fmt.Println("Timeout establishing remote connection to backend.")
		establishRemoteConn = false
		conn.Close()
		backend.Terminate()
		return
	case ok := <-remoteConnEstablishedCh:
		if !ok {
			fmt.Println("Failed to establish connection to backend.")
			conn.Close()
			backend.Terminate()
			return
		}
	}

	// Start bi-directional pipes
	var pipeMux sync.Mutex
	var pipeDone = false
	// write to dst what it reads from src
	var pipe = func(src, dst net.Conn, filter func(b *[]byte)) {
		defer func() {
			pipeMux.Lock()
			// if first pipe to end, closing conn will end the other pipe.
			if !pipeDone {
				fmt.Println("Closing pipe " + p.Addr.String() + "<->" + p.Target.String())
				conn.Close()
				rconn.Close()
				backend.Terminate()
			}
			pipeDone = true
			pipeMux.Unlock()
		}()

		buff := make([]byte, 65535)
		for {
			src.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, err := src.Read(buff)
			if err, ok := err.(net.Error); ok && err.Timeout() {
				continue
			}
			if err != nil {
				return
			}
			b := buff[:n]

			if filter != nil {
				filter(&b)
			}

			_, err = dst.Write(b)
			if err != nil {
				return
			}
		}
	}

	fmt.Println("Initiating pipe " + p.Addr.String() + "<->" + p.Target.String())
	go pipe(conn, rconn, p.Director)
	go pipe(rconn, conn, nil)
}
