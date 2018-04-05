package main

// This code comes from https://github.com/kahlys/proxy

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"os"

	"vncproxy"
)

var (
	localAddr  = flag.String("lhost", ":5900", "proxy local address")
	remoteAddr = flag.String("rhost", ":5901", "proxy remote address")
	localTLS   = flag.Bool("ltls", false, "tls/ssl between client and proxy")
	localCert  = flag.String("lcert", "", "proxy certificate x509 file for tls/ssl use")
	localKey   = flag.String("lkey", "", "proxy key x509 file for tls/ssl use")
	remoteTLS  = flag.Bool("rtls", false, "tls/ssl between proxy and target")
)

func main() {
	flag.Parse()

	laddr, err := net.ResolveTCPAddr("tcp", *localAddr)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	raddr, err := net.ResolveTCPAddr("tcp", *remoteAddr)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if *localTLS && !exists(*localCert) && !exists(*localKey) {
		fmt.Println("certificate and key file required")
		os.Exit(1)
	}

	var p = new(vncproxy.Server)
	if *remoteTLS {
		// Testing only. You needs to specify config.ServerName insteand of InsecureSkipVerify
		p = vncproxy.NewServer(raddr, nil, &tls.Config{InsecureSkipVerify: true})
	} else {
		p = vncproxy.NewServer(raddr, nil, nil)
	}

	fmt.Println("Proxying from " + laddr.String() + " to " + p.Target.String())
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
