package main

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/client"
)

func NetworkTest(dclient client.CommonAPIClient, nodes map[string]string, nodeAddr string) error {
	log.Info("Commencing network test against a cluster of %d nodes", len(nodes))
	log.Info("Local node address: %s", nodeAddr)

	for hostname, addr := range nodes {
		log.Infof("Target node hostname: %s, IP Address: %s", hostname, addr)
		time.Sleep(20 * time.Second)
	}

	return nil
}
