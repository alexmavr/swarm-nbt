package main

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
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

var udpTimeout = time.Minute
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

	// Create an ICMP Pinger
	pinger := fastping.NewPinger()
	pinger.MaxRTT = icmpMaxRTT
	pinger.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		log.Infof("ICMP: Target: %s receive, RTT: %v", addr.String(), rtt)
		_, err := icmpFile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().Nanosecond(), addr.String(), rtt.Nanoseconds()))
		if err != nil {
			log.Errorf("unable to write to ICMP results file: %s", err)
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

	// Start an HTTP Server on a separate goroutine at port httpServerPort
	errChan := make(chan error)
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
		serverAddr, err := net.ResolveUDPAddr("udp", udpNodeAddr)
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
			rlen, _, err := sock.ReadFromUDP(udpBuffer[:])
			if err != nil {
				errChan <- err
				return
			}

			// Determine if the received packet is an ACK packet from a request sent from this node
			payload := string(udpBuffer[0:rlen])
			payloadParts, ack := isAck(payload)

			// The packet is not an acknowledgement of a packet sent from this node
			// Send an ACK packet back to the return address.
			if !ack {
				packetUUID := payloadParts[0]
				remoteIP := payloadParts[1]
				log.Infof("UDP: Received SYN from %s for UUID %s, sending ACK", remoteIP, packetUUID)
				returnAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", remoteIP, udpServerPort))
				if err != nil {
					errChan <- err
					continue
				}
				conn, err := net.DialUDP("udp", nil, returnAddr)
				if err != nil {
					log.Errorf("unable to dial UDP target %s: %s", remoteIP, err)
					continue
				}
				defer conn.Close()

				payload := []byte(strings.Join([]string{
					"ACKUDP",
					packetUUID,
					remoteIP,
				}, "\t"))

				_, err = conn.Write(payload)
				if err != nil {
					errChan <- err
					return
				}
				conn.Close()
				continue
			}

			// The packet is an ACK packet from a round-trip request sent by this node.
			// Inform the pinger of the received packet
			packetUUID := payloadParts[1]
			udpPinger.ReceivedPacket(packetUUID)
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
		time.Sleep(time.Duration(rand.Intn(10)) * time.Second)
	}

	return nil
}

// UDPPinger
type UDPPinger struct {
	Targets     []*net.UDPAddr
	Outfile     *os.File
	NodeAddr    *net.UDPAddr
	SentPackets map[string]PacketInfo
	Timeout     time.Duration
}

type PacketInfo struct {
	SentTime time.Time
	Dest     *net.UDPAddr
}

func (p *UDPPinger) AddIP(ip string) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, udpServerPort))
	if err != nil {
		log.Errorf("unable to resolve UDP address: %s", ip)
		return
	}

	p.Targets = append(p.Targets, addr)
}

func (p *UDPPinger) ReceivedPacket(packetUUID string) {
	if p.SentPackets == nil {
		log.Warnf("Received ACK packet before initializing packet inventory")
		return
	}

	packetInfo, found := p.SentPackets[packetUUID]
	if !found {
		log.Warnf("Received ACK packet not present in packet inventory with UUID %s", packetUUID)
		return
	}

	// Mark the packet as received
	now := time.Now()
	rtt := now.Sub(packetInfo.SentTime)
	log.Infof("UDP: Received ACK, UUID: %s, RTT: %v", packetUUID, rtt)

	_, err := p.Outfile.WriteString(fmt.Sprintf("%d\tRECV\t%s\t%s\t%d\n", now.UnixNano(), packetUUID, packetInfo.Dest.IP.String(), rtt.Nanoseconds()))
	if err != nil {
		log.Errorf("unable to mark packet with UUID %s as received: %s", packetUUID, err)
	}

	delete(p.SentPackets, packetUUID)
}

func (p *UDPPinger) Run() {
	if p.SentPackets == nil {
		p.SentPackets = make(map[string]PacketInfo)
	}

	for _, target := range p.Targets {
		conn, err := net.DialUDP("udp", nil, target)
		if err != nil {
			log.Errorf("unable to dial UDP target %s: %s", target.String(), err)
			continue
		}
		defer conn.Close()

		// Generate a random UUID for the packet ID
		newUUID := uuid.NewV4().String()
		payload := fmt.Sprintf("%s\t%s", newUUID, p.NodeAddr.IP.String())

		log.Infof("UDP: SYN against %s, UUID: %s", target.IP.String(), newUUID)
		// Send the UDP packet and mark as a SentPacket
		_, err = conn.Write([]byte(payload))
		if err != nil {
			log.Errorf("unable to write UDP packet: %s", err)
			continue
		}
		p.SentPackets[newUUID] = PacketInfo{
			SentTime: time.Now(),
			Dest:     target,
		}
		conn.Close()
	}

	// Expire any sent packets that haven't returned for a given timeout
	now := time.Now()
	for uuid, packetInfo := range p.SentPackets {
		if packetInfo.SentTime.Add(p.Timeout).Before(now) {
			// The timeout has expired for that packet. Mark it as lost
			_, err := p.Outfile.WriteString(fmt.Sprintf("%d\tLOST\t%s\t%s\n", now.UnixNano(), uuid, packetInfo.Dest.IP.String()))
			if err != nil {
				log.Errorf("unable to record UDP packet loss for uuid %s: %s", uuid, err)
			}
			delete(p.SentPackets, uuid)
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

// isAck checks if the payload of a UDP packet contains an ACK response
func isAck(payload string) ([]string, bool) {
	parts := strings.Split(payload, "\t")
	if parts[0] == "ACKUDP" {
		return parts, true
	}
	return parts, false
}
