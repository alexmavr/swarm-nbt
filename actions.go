package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
					TargetPort:    4443,
					PublishedPort: 4443,
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
	log.SetOutput(os.Stdout)

	dclient, err := getDockerClient(c.String("docker_socket"))
	if err != nil {
		return err
	}

	ctx, cancelFunc := context.WithDeadline(context.Background(), time.Now().Add(time.Minute))
	defer cancelFunc()

	// Inspect the service to extract the list of nodes
	svc, _, err := dclient.ServiceInspectWithRaw(ctx, "swarm-nbt-service")
	if err != nil {
		return err
	}

	var nodes map[string]string
	for _, envVar := range svc.Spec.TaskTemplate.ContainerSpec.Env {
		if !strings.Contains(envVar, "NODES") {
			continue
		}

		parts := strings.Split(envVar, "=")
		if len(parts) != 2 {
			return fmt.Errorf("unexpected number of parts in env var: %s", envVar)
		}

		err = json.Unmarshal([]byte(parts[1]), &nodes)
		if err != nil {
			return err
		}
		break
	}

	// Collect the files from each node
	for hostname, ip := range nodes {
		log.Infof("Collecting logs for node %s with IP %s", hostname, ip)
		for _, file := range []string{"icmp.txt", "http.txt"} {
			resp, err := http.Get(fmt.Sprintf("http://%s:%s/%s", ip, httpServerPort, file))
			if err != nil {
				log.Errorf("unable to collect %s from node %s: %s", file, hostname, err)
			}
			httpOut, err := os.Open(fmt.Sprintf("/results/node_%s_http.txt", hostname))
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			_, err = io.Copy(httpOut, resp.Body)
			if err != nil {
				return err
			}
		}
	}

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
