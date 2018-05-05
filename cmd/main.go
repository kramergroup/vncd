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
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/kramergroup/vncd"
	"github.com/kramergroup/vncd/backends"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	configFile    = "/etc/vncd/vncd.conf.yaml"
	defaultConfig = readConfigFile(configFile)

	config = Config{
		Frontend: FrontendConfig{
			Port:       flag.Int("port", *defaultConfig.Frontend.Port, "proxy local address"),
			TLS:        flag.Bool("tls", *defaultConfig.Frontend.TLS, "tls/ssl between client and proxy"),
			Cert:       flag.String("cert", *defaultConfig.Frontend.Cert, "proxy certificate x509 file for tls/ssl use"),
			Key:        flag.String("key", *defaultConfig.Frontend.Key, "proxy key x509 file for tls/ssl use"),
			RemoteTLS:  flag.Bool("remotetls", *defaultConfig.Frontend.RemoteTLS, "tls/ssl between proxy and VNC server"),
			HealthPort: flag.Int("healthPort", *defaultConfig.Frontend.HealthPort, "health endpoint address"),
		},
		Backend: BackendConfig{
			Port:          flag.Int("backendPort", *defaultConfig.Backend.Port, "backend address"),
			Type:          flag.String("backendType", *defaultConfig.Backend.Type, "backend type"),
			Image:         flag.String("backendImage", *defaultConfig.Backend.Image, "backend address"),
			Network:       flag.String("backendNetwork", *defaultConfig.Backend.Network, "backend network"),
			Kubeconfig:    flag.String("kubeconfig", *defaultConfig.Backend.Network, "Location of the kubeconfig file"),
			LabelSelector: flag.String("labelSelector", *defaultConfig.Backend.LabelSelector, "Label selector for pods"),
			Namespace:     flag.String("namespace", *defaultConfig.Backend.Namespace, "Namespace for pods"),
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
	Port       *int    `yaml:"Port"`
	HealthPort *int    `yaml:"HealthPort"`
	TLS        *bool   `yaml:"TLS"`
	Cert       *string `yaml:"Cert"`
	Key        *string `yaml:"Key"`
	RemoteTLS  *bool   `yaml:"RemoteTLS"`
}

// BackendConfig holds backend configurartion
// Currently, this is a union of configurartion variables
// of ALL backend implementations to keep things simple
// TODO Find a better way to separate out backend
//      configurations for different backends
type BackendConfig struct {

	// Common fields
	Type *string `yaml:"Type"`
	Port *int    `yaml:"Port"`

	// Type Docker fields
	Image   *string `yaml:"Image"`
	Network *string `yaml:"Network"`

	// Kubernetes fields
	LabelSelector *string `yaml:"LabelSelector"`
	Namespace     *string `yaml:"Namespace"`
	Kubeconfig    *string `yaml:"Kubeconfig"`
}

func main() {
	flag.Parse()

	processConfig()

	laddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", *config.Frontend.Port))
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if *config.Frontend.TLS && !exists(*config.Frontend.Cert) && !exists(*config.Frontend.Key) {
		fmt.Println("certificate and key file required")
		os.Exit(1)
	}

	var p = new(vncd.Server)

	if *config.Frontend.RemoteTLS {
		// Testing only. You needs to specify config.ServerName insteand of InsecureSkipVerify
		p, err = vncd.NewServer(nil, backendFactory, &tls.Config{InsecureSkipVerify: true})
	} else {
		p, err = vncd.NewServer(nil, backendFactory, nil)
	}

	if *config.Frontend.HealthPort != 0 {
		go reportHealth(p)
	}

	fmt.Println("Listening on " + laddr.String() + " for incomming connections")
	if *config.Frontend.TLS {
		p.ListenAndServeTLS(laddr, *config.Frontend.Cert, *config.Frontend.Key)
	} else {
		p.ListenAndServe(laddr)
	}

}

// readConfigFile reads configuration variables from a global
// configuration file (provided via the -config commandline parameter)
func readConfigFile(configFile string) Config {

	var fileConfig Config
	yamlFile, err := ioutil.ReadFile(configFile)

	if err == nil {
		err = yaml.Unmarshal(yamlFile, &fileConfig)
	}

	if err != nil {
		fmt.Println("Error reading configuration from file " + configFile)
		os.Exit(1)
	}
	return fileConfig
}

func processConfig() {

	// Define backend factory method
	switch *config.Backend.Type {
	case "docker":
		backendFactory = func() (backends.Backend, error) {
			fmt.Println("Creating Docker backend with image " + *(config.Backend.Image))
			return backends.CreateDockerBackend(*(config.Backend.Image), *(config.Backend.Port), *(config.Backend.Network))
		}
	case "kubernetes":
		backendFactory = func() (backends.Backend, error) {
			fmt.Printf("Createing Kubernetes backend with label selector [%s] in namespace [%s]\n", *(config.Backend.LabelSelector), *(config.Backend.Namespace))

			var conf *rest.Config
			var err error
			if *config.Backend.Kubeconfig == "" {
				conf, err = rest.InClusterConfig()
				if err != nil {
					log.Fatalf("Could not build Kubernetes configuration [%s]", err)
				}
			} else {
				conf, err = clientcmd.BuildConfigFromFlags("", *config.Backend.Kubeconfig)
				if err != nil {
					log.Fatalf("Could not build Kubernetes configuration [%s]", err)
				}
			}

			clientset, err := kubernetes.NewForConfig(conf)
			return backends.CreateKubernetesBackend(clientset, *(config.Backend.Namespace), *(config.Backend.LabelSelector), *(config.Backend.Port))
		}
	default:
		fmt.Println("Unknown backend type: " + *config.Backend.Type)
		os.Exit(1)
	}

}

type healthHandler struct {
	Server *vncd.Server
}

func (h healthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	type Status struct {
		Acceptingconnections bool `json:"accepting"`
		Numberofconnections  int  `json:"open"`
	}

	s := Status{
		Acceptingconnections: h.Server.AcceptingConnections(),
		Numberofconnections:  h.Server.CountOpenConnections(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
	if !s.Acceptingconnections {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	fmt.Println("Handled health check")
}

func reportHealth(srv *vncd.Server) {

	haddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf(":%d", *config.Frontend.HealthPort))
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	fmt.Println("Listening for health check requests on " + haddr.String())
	err = http.ListenAndServe(haddr.String(), healthHandler{
		Server: srv,
	})
}

// exists is a small helper rerturning true if a file exists
func exists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
