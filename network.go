package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/client"
	fastping "github.com/tatsushid/go-fastping"
)

const (
	httpServerPort      = 4443
	icmpResultsFilePath = "/results/icmp.txt"
	httpResultsFilePath = "/results/http.txt"
)

// NetworkTest collects network link information from the local node against all nodes in the
// provided node inventory.
// The following tests are performed:
//		- ICMP Echo Request/Response (measurement of ICMP RTT and packet loss)
//		- HTTP Request/Response		 (measurement of TCP RTT)
func NetworkTest(dclient client.CommonAPIClient, nodes map[string]string, nodeAddr string) error {
	log.Infof("Commencing network test against a cluster of %d nodes", len(nodes))
	log.Infof("Local node address: %s", nodeAddr)

	// Open the ICMP results file
	icmpFile, err := os.OpenFile(icmpResultsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer icmpFile.Close()

	// Create an ICMP Pinger
	pinger := fastping.NewPinger()
	pinger.MaxRTT = 20 * time.Second
	pinger.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		log.Infof("ICMP: Target: %s receive, RTT: %v", addr.String(), rtt)
		_, err := icmpFile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().Nanosecond(), addr.String(), rtt.Nanoseconds()))
		if err != nil {
			log.Errorf("unable to write to ICMP results file: %s", err)
		}
	}
	pinger.OnIdle = func() {
		// MaxRTT has been exceeded
		_, err := icmpFile.WriteString("=============\n")
		if err != nil {
			log.Errorf("unable to write to ICMP results file: %s", err)
		}
		log.Infof("ICMP Pinger Idle")
	}

	// Start an HTTP Server on a separate goroutine
	go func() {
		srv := &http.Server{
			Handler: http.HandlerFunc(httpServerHandler),
			Addr:    fmt.Sprintf(":%d", httpServerPort),
		}
		srv.ListenAndServe()
	}()

	// Create an HTTP Pinger
	httpOutFile, err := os.OpenFile(httpResultsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer httpOutFile.Close()
	httpPinger := &HTTPPinger{
		TargetPort: httpServerPort,
		Outfile:    httpOutFile,
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
	}

	// Perform one round-trip ICMP ping for every node
	for {
		err = pinger.Run()
		if err != nil {
			fmt.Println(err)
		}

		httpPinger.Run()
		time.Sleep(5 * time.Second)
	}

	return nil
}

func httpServerHandler(w http.ResponseWriter, r *http.Request) {
	log.Infof("Received request from %s", r.RemoteAddr)
}

type HTTPPinger struct {
	Targets    []string
	TargetPort int
	Outfile    *os.File
}

func (p *HTTPPinger) AddURL(urlString string) {
	p.Targets = append(p.Targets, urlString)
}

func (p *HTTPPinger) Run() {
	for _, target := range p.Targets {
		startTime := time.Now()
		_, err := http.Get(target)
		if err != nil {
			log.Errorf("unable to reach http target %s: %s", target, err)
			p.Outfile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().UnixNano(), target, -1))
			continue
		}
		endTime := time.Now()
		rtt := endTime.Sub(startTime)
		log.Infof("HTTP: Target: %s receive, RTT: %v", target, rtt)
		_, err = p.Outfile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().UnixNano(), target, rtt.Nanoseconds()))
		if err != nil {
			log.Errorf("unable to write to HTTP results file: %s", err)
		}
	}
}
