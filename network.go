package main

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/client"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	fastping "github.com/tatsushid/go-fastping"
)

const (
	httpServerPort      = 4443
	udpServerPort       = 6789
	udpClientPort       = 6790
	icmpResultsFilePath = "/results/icmp.txt"
	httpResultsFilePath = "/results/http.txt"
	udpResultsFilePath  = "/results/udp.txt"

	// maxPollWaitSeconds is the maximum number of seconds to wait
	// between successive tests
	maxPollWaitSeconds = 15

	// recordFile captures measurements in a filesystem format
	recordFile = false
)

var httpTimeout = 10 * time.Second
var udpTimeout = 10 * time.Second
var udpClientTimeout = 10 * time.Second
var icmpMaxRTT = 10 * time.Second

// NetworkTest collects network link information from the local node against all nodes in the
// provided node inventory.
// The following tests are performed:
//		- ICMP RTT
//		- HTTP RTT
//		- UDP Packet Loss & RTT
func NetworkTest(dclient client.CommonAPIClient, nodes map[string]string, nodeAddr string) error {
	log.Infof("Commencing network test against a cluster of %d nodes", len(nodes))
	log.Infof("Local node address: %s", nodeAddr)

	// Open the ICMP results file
	icmpFile, err := os.OpenFile(icmpResultsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer icmpFile.Close()

	// Open the UDP results file
	udpFile, err := os.OpenFile(udpResultsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer udpFile.Close()

	// Create an error channel
	errChan := make(chan error)

	// Create an ICMP Pinger
	pinger := fastping.NewPinger()
	pinger.MaxRTT = icmpMaxRTT
	pinger.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		log.Infof("ICMP: Target: %s receive, RTT: %v", addr.String(), rtt)
		icmpRTT.WithLabelValues(addr.IP.String()).Set(rtt.Seconds())
		if recordFile {
			_, err := icmpFile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().Nanosecond(), addr.String(), rtt.Nanoseconds()))
			if err != nil {
				log.Errorf("unable to write to ICMP results file: %s", err)
			}
		}
	}
	pinger.OnIdle = func() {
		log.Debugf("ICMP Pinger Idle")
	}

	// Create a UDP Pinger
	udpNodeAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", nodeAddr, udpServerPort))
	if err != nil {
		return err
	}
	udpPinger := &UDPPinger{
		Outfile:  udpFile,
		NodeAddr: udpNodeAddr,
		Timeout:  udpTimeout,
	}

	// Start a UDP Server on a separate goroutine at port udpServerPort
	go udpPinger.StartUDPServer(errChan)

	// Start an HTTP Server on a separate goroutine at port httpServerPort
	// The HTTP File server is serving the /results directory, and is used
	// by the bootstrapper during the collection phase
	go func(errChan chan<- error) {
		router := mux.NewRouter()
		router.Handle("/metrics", promhttp.Handler())
		router.PathPrefix("/").Handler(http.FileServer(http.Dir("/results")))
		srv := &http.Server{
			Handler: router,
			Addr:    fmt.Sprintf(":%d", httpServerPort),
		}
		err := srv.ListenAndServe()
		if err != nil {
			errChan <- err
		}
	}(errChan)

	// Error handling goroutine
	go func(errChan <-chan error) {
		for {
			err := <-errChan
			log.Errorf("Error: %s", err)
		}
	}(errChan)

	// Create an HTTP Pinger
	httpOutFile, err := os.OpenFile(httpResultsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer httpOutFile.Close()
	httpPinger := &HTTPPinger{
		Outfile: httpOutFile,
	}
	// Populate the pingers with the known node inventory
	for hostname, addr := range nodes {
		if addr == "127.0.0.1" {
			log.Infof("Skipping local redirect on address %s", addr)
			continue
		}
		if hostname == "localhost" {
			log.Info("Skipping local redirect on localhost")
			continue
		}
		log.Infof("Target node hostname: %s, IP Address: %s", hostname, addr)

		ra, err := net.ResolveIPAddr("ip4:icmp", addr)
		if err != nil {
			return err
		}

		// Add the IP address to the ICMP pinger
		pinger.AddIPAddr(ra)

		// Create an HTTP URL for the HTTP pinger
		httpPinger.AddURL(fmt.Sprintf("http://%s:%d", addr, httpServerPort))

		// Create a UDP Address for the UDP pinger
		udpPinger.AddIP(addr)
	}

	// Long-lasting Client loop
	var wg sync.WaitGroup
	for {
		// ICMP Echo
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = pinger.Run()
			if err != nil {
				log.Error(err)
			}
		}()

		// HTTP GET
		wg.Add(1)
		go func() {
			defer wg.Done()
			httpPinger.Run()
		}()

		// UDP Send
		wg.Add(1)
		go func() {
			defer wg.Done()
			udpPinger.Run()
		}()

		wg.Wait()
		time.Sleep(time.Duration(rand.Intn(maxPollWaitSeconds)) * time.Second)
	}

	return nil
}
