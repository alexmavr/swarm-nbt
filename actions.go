package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

func StartBenchmark(c *cli.Context) error {
	if c.Bool("compat") {
		// When --compat is provided, the tool will expect a `docker info` plaintext blob
		// on stdin. That blob will get parsed
		log.SetOutput(os.Stderr)
		return UCPCompatibilityStart()
	}

	log.SetOutput(os.Stdout)
	dclient, err := getDockerClient(c.String("docker_socket"))
	if err != nil {
		return err
	}

	ctx, cancelFunc := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancelFunc()

	info, err := dclient.Info(ctx)
	if err != nil {
		return err
	}

	if !info.Swarm.ControlAvailable {
		return fmt.Errorf("This node is not a Swarm Manager, please start the benchmark on a swarm manager node")
	}

	// Determine the node inventory and pass it as an environment variable
	nodeInventory := make(map[string]string) // dict from hostname to IPv4 address
	nodes, err := dclient.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		return err
	}
	for _, node := range nodes {
		nodeInventory[node.Description.Hostname] = node.Status.Addr
		if node.Status.Addr == "127.0.0.1" && node.ManagerStatus != nil {
			// If the local manager node is reporting 127.0.0.1, use its manager address
			nodeInventory[node.Description.Hostname] = strings.Split(node.ManagerStatus.Addr, ":")[0]
		}
	}
	// Marshal the node inventory into a string
	nodeInvBytes, err := json.Marshal(nodeInventory)
	if err != nil {
		return err
	}
	nodeInventoryPayload := string(nodeInvBytes)

	// Start a global service that runs this image with the "agent" verb and a local volume mount
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: "swarm-nbt",
		},
		Mode: swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{
				{
					Protocol:      swarm.PortConfigProtocolTCP,
					TargetPort:    httpServerPort,
					PublishedPort: httpServerPort,
					PublishMode:   "host",
				},
				{
					Protocol:      swarm.PortConfigProtocolUDP,
					TargetPort:    udpServerPort,
					PublishedPort: udpServerPort,
					PublishMode:   "host",
				},
				{
					Protocol:      swarm.PortConfigProtocolUDP,
					TargetPort:    udpClientPort,
					PublishedPort: udpClientPort,
					PublishMode:   "host",
				},
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: swarm.ContainerSpec{
				Image:   "alexmavr/swarm-nbt:latest",
				Command: []string{"/go/bin/swarm-nbt", "agent"},
				Env:     []string{fmt.Sprintf("NODES=%s", nodeInventoryPayload)},
				Mounts: []mount.Mount{
					// Mount a volume for result data
					mount.Mount{
						Type:   mount.TypeVolume,
						Source: "swarm-nbt-results",
						Target: "/results",
					},
					// Bind-mount the docker socket
					mount.Mount{
						Type:   mount.TypeBind,
						Source: "/var/run/docker.sock",
						Target: "/var/run/docker.sock",
					},
				},
			},
		},
	}
	_, err = dclient.ServiceCreate(ctx, spec, types.ServiceCreateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// StopBenchmark determines the list of nodes, contacts the http server on each node
// and collects all benchmark results. It then calls the process method of each result type
func StopBenchmark(c *cli.Context) error {
	if c.Bool("compat") {
		return UCPCompatibilityStop()
	}

	dclient, err := getDockerClient(c.String("docker_socket"))
	if err != nil {
		return err
	}

	log.SetOutput(os.Stdout)

	ctx, cancelFunc := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancelFunc()

	// Remove the service
	err = dclient.ServiceRemove(ctx, "swarm-nbt")
	if err != nil {
		return err
	}

	return nil
}

func NodeAgent(c *cli.Context) error {
	dclient, err := getDockerClient(c.String("docker_socket"))
	if err != nil {
		return err
	}

	nodesJson := c.String("nodes")
	if nodesJson == "" {
		return fmt.Errorf("empty node inventory received")
	}

	var nodes []*Node
	err = json.Unmarshal([]byte(nodesJson), &nodes)
	if err != nil {
		return err
	}

	ctx, cancelFunc := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancelFunc()

	info, err := dclient.Info(ctx)
	if err != nil {
		return err
	}

	// discover the local node
	localNode := &Node{
		Hostname:  info.Name,
		Address:   info.Swarm.NodeAddr,
		IsManager: info.Swarm.ControlAvailable,
	}

	return NetworkTest(dclient, nodes, localNode)
}

func getDockerClient(dockerSocket string) (client.CommonAPIClient, error) {
	if dockerSocket == "" {
		return nil, fmt.Errorf("empty docker socket provided")
	}
	return client.NewClient(fmt.Sprintf("unix://%s", dockerSocket), "1.24", nil, nil)
}
