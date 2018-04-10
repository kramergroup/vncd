package backends

import (
	"context"
	"fmt"
	"net"
	"strconv"
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
	Image       string // container type to be instantiated
	Port        int    // exported port of the container
	containerID string // ID of the created container
	target      net.TCPAddr
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
	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		fmt.Println("Error obtaining Docker environment. There might be ramnant containers!")
	}
	fmt.Print("Stopping container ", b.containerID, "... ")
	if err := cli.ContainerStop(ctx, b.containerID, nil); err != nil {
		fmt.Println(err)
	}
	fmt.Println("Done")
}

/******************************************************************************
  Implementation
 ******************************************************************************/

// CreateDockerBackend creates the Docker container backend
func CreateDockerBackend(image string, port int) (Backend, error) {
	b := &DockerBackend{
		Image: image,
		Port:  port,
	}

	ctx := context.Background()
	cli, err := client.NewEnvClient()
	if err != nil {
		return b, err
	}

	pullCh := make(chan bool)
	fmt.Print("Pulling docker image " + image + " ")
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

	//out, err := cli.ImagePull(ctx, image, types.ImagePullOptions{})
	//io.Copy(os.Stdout, out)
	pullCh <- (err == nil)
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

	// Get a free host port
	// TODO : The interface should be selectable (its actually a good idea to use
	//        the loop interface rather than all interfaces, but that has issues
	//        with debuggin on Mac (docker in VM))
	hostPort, err := GetFreePort()
	hostPort.IP = net.IPv4zero // Override local IP address to listen on all interfaces
	if err != nil {
		return b, err
	}
	b.target = *hostPort
	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{
				{
					HostIP:   hostPort.IP.String(),
					HostPort: strconv.Itoa(hostPort.Port),
				},
			},
		},
	}

	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, "")
	if err != nil {
		return b, err
	}
	b.containerID = resp.ID

	if err := cli.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return b, err
	}

	fmt.Println("Created docker container " + resp.ID)
	return b, nil
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
