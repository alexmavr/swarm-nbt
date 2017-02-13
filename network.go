package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/client"
	uuid "github.com/satori/go.uuid"
	fastping "github.com/tatsushid/go-fastping"
)

const (
	httpServerPort      = 4443
	udpServerPort       = 6789
	icmpResultsFilePath = "/results/icmp.txt"
	httpResultsFilePath = "/results/http.txt"
	udpResultsFilePath  = "/results/udp.txt"
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

	// Open the UDP results file
	udpFile, err := os.OpenFile(udpResultsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend)
	if err != nil {
		return err
	}
	defer udpFile.Close()

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

	errChan := make(chan error)
	// Start an HTTP Server on a separate goroutine at port httpServerPort
	go func(errChan chan<- error) {
		srv := &http.Server{
			Handler: http.FileServer(http.Dir("/results")),
			Addr:    fmt.Sprintf(":%d", httpServerPort),
		}
		err := srv.ListenAndServe()
		if err != nil {
			errChan <- err
		}
	}(errChan)

	// Start a UDP Server on a separate goroutine at port udpServerPort
	go func(errChan chan<- error) {
		serverAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", udpServerPort))
		if err != nil {
			errChan <- err
			return
		}
		sock, err := net.ListenUDP("udp", serverAddr)
		if err != nil {
			errChan <- err
			return
		}
		defer sock.Close()
		sock.SetReadBuffer(1048576)

		var udpBuffer [1024]byte
		for {
			rlen, returnAddr, err := sock.ReadFromUDP(udpBuffer[:])
			if err != nil {
				errChan <- err
				return
			}

			// Read the packet ID from the payload
			packetUUID := string(udpBuffer[0:rlen])

			// Record the time when the UDP packet was received
			_, err = udpFile.WriteString(fmt.Sprintf("RECV\t%s\tSRC\t%s\t%d\n", packetUUID, returnAddr.IP.String(), time.Now().UnixNano()))
			if err != nil {
				errChan <- err
			}
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

	// Create a UDP Pinger
	udpPinger := &UDPPinger{
		Outfile: udpFile,
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
	for {
		// ICMP Echo
		err = pinger.Run()
		if err != nil {
			fmt.Println(err)
		}

		// HTTP GET
		httpPinger.Run()
		time.Sleep(5 * time.Second)

		// UDP Send
	}

	return nil
}

// UDPPinger
type UDPPinger struct {
	Targets []*net.UDPAddr
	Outfile *os.File
}

func (p *UDPPinger) AddIP(ip string) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, udpServerPort))
	if err != nil {
		log.Errorf("unable to resolve UDP address: %s", ip)
		return
	}

	p.Targets = append(p.Targets, addr)
}

func (p *UDPPinger) Run() {
	for _, target := range p.Targets {
		conn, err := net.DialUDP("udp", nil, target)
		if err != nil {
			log.Errorf("unable to dial UDP target %s: %s", target.String(), err)
			continue
		}

		// Generate a unique packetID
		newUUID := uuid.NewV4()
		_, err = conn.Write([]byte(newUUID.String()))
		if err != nil {
			log.Errorf("unable to write UDP packet: %s", err)
			continue
		}
		// The packet was sent out successfully, note down the sending
		_, err = p.Outfile.WriteString(fmt.Sprintf("SEND\t%s\tTGT\t%s\t%d\n", newUUID, target.IP.String(), time.Now().UnixNano()))
		if err != nil {
			log.Errorf("unable to record UDP packet send to log file: %s", err)
			continue
		}
	}
}

// HTTPPinger
type HTTPPinger struct {
	Targets []string
	Outfile *os.File
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
