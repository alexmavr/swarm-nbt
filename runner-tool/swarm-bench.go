package main

import (
	"os"

	log "github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
)

var cmdStart = cli.Command{
	Name:  "start",
	Usage: "Start a swarm network benchmark",
	Description: `
	`,
	Action: StartBenchmark,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "docker_socket",
			Usage:  "The path where the docker socket is located within this container",
			EnvVar: "DOCKER_SOCKET",
			Hidden: true,
		},
	},
}

var cmdStop = cli.Command{
	Name:  "stop",
	Usage: "Stop a swarm network benchmark, collect the results the process them",
	Description: `
	`,
	Action: StopBenchmark,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "docker_socket",
			Usage:  "The path where the docker socket is located within this container",
			EnvVar: "DOCKER_SOCKET",
			Hidden: true,
		},
	},
}

var cmdPause = cli.Command{
	Name:  "pause",
	Usage: "Pause a swarm network benchmark. Running the `start` operation will resume the benchmark",
	Description: `
	`,
	Action: StopBenchmark,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "docker_socket",
			Usage:  "The path where the docker socket is located within this container",
			EnvVar: "DOCKER_SOCKET",
			Hidden: true,
		},
	},
}

var cmdAgent = cli.Command{
	Name:  "agent",
	Usage: "Start a local node agent for network metric collection",
	Description: `
	`,
	Action: StartBenchmark,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "docker_socket",
			Usage:  "The path where the docker socket is located within this container",
			EnvVar: "DOCKER_SOCKET",
			Hidden: true,
		},
	},
}

// Driver function
func main() {
	app := cli.NewApp()
	app.Name = "Swarm Network Benchmark Tool"
	app.Usage = "Start, Pause or Stop a Networking Benchmark"
	app.Commands = []cli.Command{
		cmdStart,
		cmdPause,
		cmdStop,
		cmdAgent,
	}
	app.Version = "1.0.0"
	log.SetFormatter(&log.JSONFormatter{})

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
}
