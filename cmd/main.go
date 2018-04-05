package main

// This code comes from https://github.com/kahlys/proxy

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/kramergroup/vncd"
)

var (
	localAddr = flag.String("port", ":5900", "proxy local address")
	localTLS  = flag.Bool("tls", false, "tls/ssl between client and proxy")
	localCert = flag.String("cert", "", "proxy certificate x509 file for tls/ssl use")
	localKey  = flag.String("key", "", "proxy key x509 file for tls/ssl use")
	remoteTLS = flag.Bool("remotetls", false, "tls/ssl between proxy and VNC server")
	vncScript = flag.String("vncscript", "/etc/vncd/startvnc.sh", "Shell script starting VNC server")
	bootstrap = flag.String("bootstap", "", "Shell script bootstrapping environment")
)

func main() {
	flag.Parse()

	laddr, err := net.ResolveTCPAddr("tcp", *localAddr)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if *localTLS && !exists(*localCert) && !exists(*localKey) {
		fmt.Println("certificate and key file required")
		os.Exit(1)
	}

	if *bootstrap != "" && !exists(*bootstrap) {
		fmt.Println("Bootstap file does not exist")
		os.Exit(1)
	}

	if *vncScript != "" && !exists(*vncScript) {
		fmt.Println("Warning: VNC start script does not exist")
	}

	factory := func() vncd.VncSession {
		s, err2 := vncd.NewDefaultVncSessionWithScripts(*vncScript, *bootstrap)
		if err2 == nil {
			return s
		}

		return vncd.NewFallbackVncSession()
	}

	var p = new(vncd.Server)

	if *remoteTLS {
		// Testing only. You needs to specify config.ServerName insteand of InsecureSkipVerify
		p, err = vncd.NewServer(nil, factory, &tls.Config{InsecureSkipVerify: true})
	} else {
		p, err = vncd.NewServer(nil, factory, nil)
	}

	fmt.Println("Listening on " + laddr.String() + " for incomming connections")
	if *localTLS {
		p.ListenAndServeTLS(laddr, *localCert, *localKey)
	} else {
		p.ListenAndServe(laddr)
	}
}

func exists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
