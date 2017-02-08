package main

import (
	"context"
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

	// Start a global service that runs this image with the "agent" verb and a local volume mount
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: "swarm-benchnet-service",
		},
		Mode: swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: swarm.ContainerSpec{
				Image:   "alexmavr/swarm-benchnet:latest",
				Command: []string{"agent"},
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
	log.Info("unimplemented, cannot pause yet")
	return nil
}

func NodeAgent(c *cli.Context) error {
	return nil
}

func getDockerClient(dockerSocket string) (client.CommonAPIClient, error) {
	return client.NewClient(fmt.Sprintf("unix://%s", dockerSocket), "1.24", nil, nil)
}
