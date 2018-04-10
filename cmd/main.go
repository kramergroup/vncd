package main

/*
 	VNCD is a proxy for long sessions. It is intended as a
	multiplexer for internet services.

	Function:
	The multiplexer listen on a port for incoming connections.
	When a connection is establised, the multiplexer calls a
	factory function to obtain a handling service. This is normally
	intended to be a docker container exposing an internet service
	at a predefined port. The mux then acts as proxy between the
	incomming connection and the backend.

  This code is based on code from https://github.com/kahlys/proxy
*/

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"

	"github.com/kramergroup/vncd"
	"github.com/kramergroup/vncd/backends"
	yaml "gopkg.in/yaml.v2"
)

var (
	configFile = flag.String("config", "etc/vncd.conf.yaml", "Location of the configuration file")
	config     = Config{
		Frontend: FrontendConfig{
			Port:      *flag.Int("port", 5900, "proxy local address"),
			TLS:       *flag.Bool("tls", false, "tls/ssl between client and proxy"),
			Cert:      *flag.String("cert", "", "proxy certificate x509 file for tls/ssl use"),
			Key:       *flag.String("key", "", "proxy key x509 file for tls/ssl use"),
			RemoteTLS: *flag.Bool("remotetls", false, "tls/ssl between proxy and VNC server"),
		},
	}
	backendFactory func() (backends.Backend, error)
)

// Config holds to global configuration of the proxy
type Config struct {
	Frontend FrontendConfig `yaml:"Frontend"`
	Backend  BackendConfig  `yaml:"Backend"`
}

// FrontendConfig contains the front-end related configuration
type FrontendConfig struct {
	Port      int    `yaml:"Port"`
	TLS       bool   `yaml:"TLS"`
	Cert      string `yaml:"Cert"`
	Key       string `yaml:"Key"`
	RemoteTLS bool   `yaml:"RemoteTLS"`
}

// BackendConfig holds backend configurartion
// Currently, this is a union of configurartion variables
// of ALL backend implementations to keep things simple
// TODO Find a better way to separate out backend
//      configurations for different backends
type BackendConfig struct {
	Type string `yaml:"Type"`

	// Type Docker fields
	Port  int    `yaml:"Port"`
	Image string `yaml:"Image"`
}

func main() {
	flag.Parse()

	if exists(*configFile) {
		processConfig(*configFile)
	} else {
		fmt.Println("Configuration file " + *configFile + " does not exists")
	}

	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", config.Frontend.Port))
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if config.Frontend.TLS && !exists(config.Frontend.Cert) && !exists(config.Frontend.Key) {
		fmt.Println("certificate and key file required")
		os.Exit(1)
	}

	if exists(*configFile) {
		processConfig(*configFile)
	} else {
		fmt.Println("Configuration file " + *configFile + " does not exists")
	}
	var p = new(vncd.Server)

	if config.Frontend.RemoteTLS {
		// Testing only. You needs to specify config.ServerName insteand of InsecureSkipVerify
		p, err = vncd.NewServer(nil, backendFactory, &tls.Config{InsecureSkipVerify: true})
	} else {
		p, err = vncd.NewServer(nil, backendFactory, nil)
	}

	fmt.Println("Listening on " + laddr.String() + " for incomming connections")
	if config.Frontend.TLS {
		p.ListenAndServeTLS(laddr, config.Frontend.Cert, config.Frontend.Key)
	} else {
		p.ListenAndServe(laddr)
	}
}

// processConfig reads configuration variables from a global
// configuration file (provided via the -config commandline parameter)
func processConfig(configFile string) {

	yamlFile, err := ioutil.ReadFile(configFile)

	if err == nil {
		err = yaml.Unmarshal(yamlFile, &config)
	}

	if err != nil {
		fmt.Println("Error reading configuration from file " + configFile)
		os.Exit(1)
	}

	switch config.Backend.Type {
	case "docker":
		backendFactory = func() (backends.Backend, error) {
			fmt.Println("Creating Docker backend with image " + config.Backend.Image)
			return backends.CreateDockerBackend(config.Backend.Image, config.Backend.Port)
		}
	default:
		fmt.Println("Unknown backend type: " + config.Backend.Type)
		os.Exit(1)
	}

}

// exists is a small helper rerturning true if a file exists
func exists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
