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

func getNodeInventoryFromInfoStdin() ([]*Node, error) {
	// In 1.12, this tool is only operable under a classic swarm cluster
	scanner := bufio.NewScanner(os.Stdin)

	// Extract the node inventory from the docker info output
	nodeInventory := make(map[string]*Node) // dict from hostname to Node entries
	flatNodeInventory := []*Node{}
	expectedNodes := 0
	parsingNodes := false
	parsingManagers := false
	var err error
	for scanner.Scan() {
		// Parse the "Nodes" section
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
				return flatNodeInventory, err
			}
			parsingNodes = true
			continue
		}

		if strings.Contains(scanner.Text(), "└") {
			continue
		}

		if parsingNodes {
			if strings.Contains(scanner.Text(), "Cluster Managers") {
				parsingManagers = true
				parsingNodes = false
				continue
			}
			// We are expecting node entries as "hostname: ip:port")
			parts := strings.Split(scanner.Text(), ":")
			log.Info(parts)
			if len(parts) != 3 {
				break
			}
			hostname := strings.Trim(parts[0], " ")
			ip := strings.Trim(parts[1], " ")
			nodeInventory[hostname] = &Node{
				Hostname: hostname,
				Address:  ip,
			}
		}

		if parsingManagers {
			// We are expecting node managers as "hostname: Healthy")
			parts := strings.Split(scanner.Text(), ":")
			log.Info(parts)
			if len(parts) != 2 {
				break
			}
			hostname := strings.Trim(parts[0], " ")
			log.Info("attempting to mark manager with hostname %s")
			mgr, ok := nodeInventory[hostname]
			if !ok {
				log.Errorf("Manager with hostname %s was not found in the node inventory with that hostname", hostname)
				break
			}
			mgr.IsManager = true
		}
	}

	log.Infof("extracted %d nodes from info blob", len(nodeInventory))
	if expectedNodes != len(nodeInventory) {
		log.Warnf("we were expected to extract %d nodes instead", expectedNodes)
	}

	// Flatten node inventory map to a list
	for _, node := range nodeInventory {
		flatNodeInventory = append(flatNodeInventory, node)
	}

	return flatNodeInventory, nil
}

func UCPCompatibilityStart() error {
	log.Info("extracting nodes from info blob")
	nodeInventory, err := getNodeInventoryFromInfoStdin()
	if err != nil {
		return err
	}

	// Marshal the node inventory into a string
	nodeInvBytes, err := json.Marshal(nodeInventory)
	if err != nil {
		return err
	}
	nodeInventoryPayload := string(nodeInvBytes)

	prometheusInventory := "- targets: [ "
	acc := "\ndocker volume create swarm-nbt-results &&\n"
	for _, node := range nodeInventory {
		acc = fmt.Sprintf("%sdocker run -v /var/run/docker.sock:/var/run/docker.sock -v swarm-nbt-results:/results -e constraint:node==%s -d -p 3443:3443 -p 6789:6789/udp -e NODES='%s' --label swarm.benchmark.tool=agent alexmavr/swarm-nbt:latest agent && \n", acc, node.Hostname, nodeInventoryPayload)
		if err != nil {
			return err
		}
		prometheusInventory += fmt.Sprintf("\"%s:%d\",", node.Address, httpServerPort)
	}
	// Strip the last comma and append a right square bracket
	prometheusInventory = prometheusInventory[:len(prometheusInventory)-1] + " ]\n"

	// Write the node inventory to the expected location for prometheus to pick it up
	invF, err := os.OpenFile("/inventory/inventory.yml", os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer invF.Close()
	_, err = invF.Write([]byte(prometheusInventory))
	if err != nil {
		return err
	}

	acc = acc + "docker run -d -p 9090:9090 -v inventory:/inventory alexmavr/swarm-nbt-prometheus:latest && docker run -d -p 3000:3000 grafana/grafana \n"
	fmt.Print(acc)
	return nil
}

// UCPCompatibilityStop collects the results from all containers
func UCPCompatibilityStop() error {
	// Return a container removal operation on stdout
	acc := "docker ps --filter label=swarm.benchmark.tool=agent -q | xargs -n 1 docker rm -f"
	fmt.Print(acc)
	return nil
}
