package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

func StartBenchmark(c *cli.Context) error {
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
			Name: "swarm-benchnet-service",
		},
		Mode: swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{
				{
					Protocol:      swarm.PortConfigProtocolTCP,
					TargetPort:    4443,
					PublishedPort: 4443,
				},
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: swarm.ContainerSpec{
				Image:   "alexmavr/swarm-benchnet:latest",
				Command: []string{"/go/bin/swarm-benchnet", "agent"},
				Env:     []string{fmt.Sprintf("NODES=%s", nodeInventoryPayload)},
				Mounts: []mount.Mount{
					// Mount a volume for result data
					mount.Mount{
						Type:   mount.TypeVolume,
						Source: "swarm-benchnet-results",
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

func StopBenchmark(c *cli.Context) error {
	dclient, err := getDockerClient(c.String("docker_socket"))
	if err != nil {
		return err
	}

	ctx, cancelFunc := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancelFunc()

	err = dclient.ServiceRemove(ctx, "swarm-benchnet-service")
	if err != nil {
		return err
	}

	// TODO: collect results and pretty-print
	return nil
}

func PauseBenchmark(c *cli.Context) error {
	log.Info("unimplemented")
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

	var nodes map[string]string
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

	return NetworkTest(dclient, nodes, info.Swarm.NodeAddr)
}

func getDockerClient(dockerSocket string) (client.CommonAPIClient, error) {
	if dockerSocket == "" {
		return nil, fmt.Errorf("empty docker socket provided")
	}
	return client.NewClient(fmt.Sprintf("unix://%s", dockerSocket), "1.24", nil, nil)
}
