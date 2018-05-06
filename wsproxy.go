package vncd

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/websocket"

	"github.com/kramergroup/vncd/backends"
)

// WebsocketServer is a WS server that takes an incoming request and sends it to another
// servers TCP port, proxying the response back to the client.
type WebsocketServer struct {
	// Creator creates a new Backend for connection requests
	BackendFactory func() (backends.Backend, error)

	// Pipe termination channels
	sigs map[chan<- os.Signal]struct{}

	// Status of the proxy - true if ready to accept connections
	accepting bool

	// Use binary mode for communication
	binaryMode bool
}

// NewWebsocketServer created a new proxy which sends all packet to target. The function dir
// intercept and can change the packet before sending it to the target.
func NewWebsocketServer(factory func() (backends.Backend, error)) (*WebsocketServer, error) {

	p := &WebsocketServer{
		BackendFactory: factory,
		sigs:           make(map[chan<- os.Signal]struct{}),
		binaryMode:     true,
	}

	var err error
	if factory == nil {
		err = errors.New("Backend factory method must not be nil")
	}
	return p, err
}

// ListenAndServe listens on the TCP network address laddr and then handle packets
// on incoming connections.
func (p *WebsocketServer) ListenAndServe(laddr *net.TCPAddr) {

	p.accepting = true
	defer func() {
		p.accepting = false
	}()

	handler := func(ws *websocket.Conn) {
		p.relayHandler(ws)
	}

	http.Handle("/", websocket.Handler(handler))
	log.Fatal(http.ListenAndServe(laddr.String(), nil))
}

func (p *WebsocketServer) relayHandler(ws *websocket.Conn) {

	var backend *backends.Backend
	var err error
	var target *net.TCPAddr
	var conn net.Conn

	// Initiate the backend
	backend, err = p.createBackend()
	if err != nil {
		log.Printf(err.Error())
		ws.Close()
		return
	}
	defer (*backend).Terminate()

	target, err = (*backend).GetTarget()
	if err != nil {
		log.Printf("Could not get backend target [%v] \n", err)
		ws.Close()
		return
	}

	conn, err = p.dialConnection(target.String())
	if err != nil {
		log.Printf("Could not open connection to backend %v \n", err)
		ws.Close()
		return
	}

	if p.binaryMode {
		ws.PayloadType = websocket.BinaryFrame
	}

	log.Println("Starting websocket pipe to " + target.String())
	doneCh := make(chan bool)

	go copyWorker(ws, conn, doneCh)
	go copyWorker(conn, ws, doneCh)

	<-doneCh
	log.Println("Closing websocket pipe to " + target.String())
	conn.Close()
	ws.Close()
	<-doneCh
}

func (p *WebsocketServer) dialConnection(target string) (net.Conn, error) {
	// connects to VNC server - try for 5 seconds to give time for VNC to come up
	var rconn net.Conn
	var establishRemoteConn = true
	remoteConnEstablishedCh := make(chan bool)
	go func() {
		var err error
		for establishRemoteConn {
			// if p.Config == nil {
			// 	rconn, err = net.Dial("tcp", target)
			// 	establishRemoteConn = (err != nil)
			// } else {
			// 	rconn, err = tls.Dial("tcp", target, p.Config)
			// 	establishRemoteConn = (err != nil)
			// }
			rconn, err = net.Dial("tcp", target)
			establishRemoteConn = (err != nil)
		}
		remoteConnEstablishedCh <- (err == nil)
	}()

	select {
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("Timeout connecting to TCP port")
	case ok := <-remoteConnEstablishedCh:
		if !ok {
			return nil, fmt.Errorf("Failed to establish connection to backend")
		}
	}
	return rconn, nil
}

func (p *WebsocketServer) createBackend() (*backends.Backend, error) {
	// Initiate the backend
	backendCreatedCh := make(chan bool)
	var backend backends.Backend
	go func() {
		var err error
		backend, err = p.BackendFactory()
		if err != nil {
			log.Println(err)
		}
		backendCreatedCh <- (err == nil)
	}()

	select {
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("Timeout obtaining backend")
	case ok := <-backendCreatedCh:
		if !ok {
			return nil, errors.New("Failed to obtain backend")
		}
		return &backend, nil
	}
}

func copyWorker(dst net.Conn, src net.Conn, doneCh chan<- bool) {

	for {
		_, err := io.Copy(dst, src)
		if err != nil {
			log.Printf(err.Error())
			break
		}
	}
	doneCh <- true
}
