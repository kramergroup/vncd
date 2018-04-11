package backends

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

/*
DockerBackend implements a local Backend that spawns a new Docker container
locally to handle the request
*/
type DockerBackend struct {
	Image            string // container type to be instantiated
	Port             int    // exported port of the container
	containerID      string // ID of the created container
	dockerNetwork    string // Docker network name used for isolation
	target           net.TCPAddr
	cli              *client.Client
	ctx              context.Context
	containerRunning bool
	termMux          sync.Mutex
}

/*
 ------------------------------------------------------------------------------
  Backend interface
 ------------------------------------------------------------------------------
*/

// GetTarget returns the internet address of the backing container
func (b *DockerBackend) GetTarget() *net.TCPAddr {
	return &b.target
}

// Terminate removes the backing container
func (b *DockerBackend) Terminate() {

	b.termMux.Lock()

	if !b.containerRunning {
		return
	}

	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		fmt.Println("Error obtaining Docker environment. There might be ramnant containers!")
	}
	fmt.Print("Stopping container ", b.containerID, "... ")

	if err = cli.ContainerStop(ctx, b.containerID, nil); err != nil {
		fmt.Println(err)
	}
	b.containerRunning = (err != nil)
	b.termMux.Unlock()
	fmt.Println("Done")
}

/******************************************************************************
  Implementation
 ******************************************************************************/

// CreateDockerBackend creates the Docker container backend
func CreateDockerBackend(image string, port int, network string) (Backend, error) {
	b := &DockerBackend{
		Image:            image,
		Port:             port,
		dockerNetwork:    network,
		ctx:              context.Background(),
		containerRunning: false,
	}

	var err error
	b.cli, err = client.NewEnvClient()
	if err != nil {
		return b, err
	}

	containerPort := nat.Port(fmt.Sprintf("%d/tcp", port))
	containerConfig := &container.Config{
		Image: image,
		ExposedPorts: nat.PortSet{
			containerPort: struct{}{},
		},
	}

	var hostConfig *container.HostConfig
	runningInContainer, cID := runningInsideContainer()
	if runningInContainer == true {
		if b.dockerNetwork == "" {
			fmt.Println("Connecting through docker default bridge")
			// Default hostconfig is fine for this
		} else {
			// TODO: Make sure network exists
			// TODO: Attach proxy to network (if needed)
			fmt.Println("Attaching " + cID + " to network ")
			// TODO: Configure hostConfig to use network
		}
	} else {
		fmt.Println("Exposing external port")
		// Get a free host port
		// TODO : The interface should be selectable (its actually a good idea to use
		//        the loop interface rather than all interfaces, but that has issues
		//        with debuggin on Mac (docker in VM))
		var hostPort *net.TCPAddr
		hostPort, err = GetFreePort()
		if err != nil {
			fmt.Println("No free port on host")
			return b, err
		}
		hostPort.IP = net.IPv4zero // Override local IP address to listen on all interfaces
		if err != nil {
			return b, err
		}
		b.target = *hostPort
		hostConfig = &container.HostConfig{
			PortBindings: nat.PortMap{
				containerPort: []nat.PortBinding{
					{
						HostIP:   hostPort.IP.String(),
						HostPort: strconv.Itoa(hostPort.Port),
					},
				},
			},
		}
	}

	resp, err := b.cli.ContainerCreate(b.ctx, containerConfig, hostConfig, nil, "")
	if err != nil {
		if err = b.pullImage(); err != nil {
			return b, err
		}
		resp, err = b.cli.ContainerCreate(b.ctx, containerConfig, hostConfig, nil, "")
		return b, err
	}
	b.containerID = resp.ID

	if err = b.cli.ContainerStart(b.ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return b, err
	}
	b.containerRunning = true
	fmt.Println("Created docker container " + resp.ID)

	// Obtain container IP if not running on host network
	if runningInContainer {
		var containerIP string
		var addr *net.TCPAddr
		containerIP, err = b.getContainerIP(b.containerID)
		if err != nil {
			return b, err
		}
		addr, err = net.ResolveTCPAddr("tcp", containerIP+":"+strconv.Itoa(port))
		if err != nil {
			return b, err
		}
		b.target = *addr
	}
	fmt.Println("Container listining on " + b.GetTarget().String())

	// Start a watcher to remove container if proxy is killed
	// sigs := make(chan os.Signal, 1)
	// signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// go func() {
	// 	for b.containerRunning {
	// 		<-sigs
	// 		b.Terminate()
	// 	}
	// }()
	return b, nil
}

func (b *DockerBackend) pullImage() error {

	pullCh := make(chan bool)
	fmt.Print("Pulling docker image " + b.Image + " ")
	go func() {
		for {
			select {
			case ok := <-pullCh:
				if ok {
					fmt.Println(" Done")
				}
				return
			case <-time.After(time.Second):
				fmt.Print(".")
			}
		}
	}()

	out, err := b.cli.ImagePull(b.ctx, b.Image, types.ImagePullOptions{})
	io.Copy(os.Stdout, out)
	pullCh <- (err == nil)

	return err
}

// GetFreePort asks the kernel for a free open port that is ready to use.
// Source: 	"github.com/phayes/freeport"
func GetFreePort() (*net.TCPAddr, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr), nil
}

// RunningInsideContainer returns true if we run inside a container
// Source: https://stackoverflow.com/questions/20010199/how-to-determine-if-a-process-runs-inside-lxc-docker
func runningInsideContainer() (bool, string) {

	cgroup, err := os.Open("/proc/1/cgroup")
	if err != nil {
		return false, ""
	}
	defer cgroup.Close()

	scanner := bufio.NewScanner(cgroup)
	for success := scanner.Scan(); success == true; {
		line := scanner.Text()
		d := strings.Split(strings.Split(line, ":")[2], "/")
		if d[1] == "docker" {
			return true, d[2]
		}
	}
	return false, ""
}

func (b *DockerBackend) getContainerIP(contID string) (string, error) {
	resp, err := b.cli.ContainerInspect(b.ctx, contID)
	if err != nil {
		return "", err
	}

	return resp.NetworkSettings.DefaultNetworkSettings.IPAddress, nil
}

func ensureContainerNetwork(contID string) {

}
