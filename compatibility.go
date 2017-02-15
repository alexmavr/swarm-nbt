package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
)

func UCPCompatibilityMode() error {
	// In 1.12, this tool is only operable under a classic swarm cluster
	scanner := bufio.NewScanner(os.Stdin)

	// Extract the node inventory from the docker info output
	nodeInventory := make(map[string]string) // dict from hostname to IPv4 address
	expectedNodes := 0
	parsingNodes := false
	var err error
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "Nodes:") {
			// There's also a "Nodes:" segment under swarm.Info, but it has no
			// leading whitespace
			parts := strings.Split(scanner.Text(), " ")
			if len(parts) != 2 {
				log.Info("detected unexpected Nodes segment: %s", scanner.Text())
				continue
			}
			expectedNodes, err = strconv.Atoi(parts[1])
			if err != nil {
				log.Info(scanner.Text())
				return err
			}
			parsingNodes = true
			continue
		}

		if strings.Contains(scanner.Text(), "└") {
			continue
		}

		if parsingNodes {
			// We are expecting node entries as "hostname: ip:port")
			parts := strings.Split(scanner.Text(), ":")
			log.Info(parts)
			if len(parts) != 3 {
				log.Info("I don't like these parts")
				break
			}
			nodeInventory[strings.Trim(parts[0], " ")] = strings.Trim(parts[1], " ")
		}
	}
	log.Infof("extracted %d nodes from info blob", len(nodeInventory))
	if expectedNodes != len(nodeInventory) {
		log.Warnf("we were expected to extract %d nodes instead", expectedNodes)
	}

	// Marshal the node inventory into a string
	nodeInvBytes, err := json.Marshal(nodeInventory)
	if err != nil {
		return err
	}
	nodeInventoryPayload := string(nodeInvBytes)

	log.Info("emitting docker run command")
	acc := "\ndocker volume create swarm-nbt-results\n"
	for hostname := range nodeInventory {
		acc = fmt.Sprintf("%s docker run -v /var/run/docker.sock:/var/run/docker.sock -v swarm-nbt-results:/results -e constraint:node==%s -d --rm -p 4443:4443 -p 6789:6789/udp -e NODES='%s' --label swarm.benchmark.tool=agent alexmavr/swarm-nbt:latest agent && \n", acc, hostname, nodeInventoryPayload)
	}
	acc = acc + "echo done"
	fmt.Print(acc)
	return nil
}
