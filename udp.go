package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	uuid "github.com/satori/go.uuid"
)

// UDPPinger
type UDPPinger struct {
	Targets  []*udpTarget
	Outfile  *os.File
	NodeAddr *net.UDPAddr
	Timeout  time.Duration
}

type udpTarget struct {
	Addr *net.UDPAddr
	Conn *net.UDPConn
}

func (p *UDPPinger) AddIP(ip string) {
	// It's not currently possible to respond to the client socket when communicating with
	// a server on the same container. For this reason, silently skip the same from
	// the targets list
	if ip == p.NodeAddr.IP.String() {
		return
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, udpServerPort))
	if err != nil {
		log.Errorf("unable to resolve UDP address: %s", ip)
		return
	}

	p.Targets = append(p.Targets, &udpTarget{
		Addr: addr,
		Conn: nil,
	})
}

func (p *UDPPinger) ReceivedPacket(packetUUID string, startTime time.Time, targetIP string) {
	// Mark the packet as received
	now := time.Now()
	rtt := now.Sub(startTime)
	log.Infof("UDP: Received ACK, UUID: %s, RTT: %v", packetUUID, rtt)
	_, err := p.Outfile.WriteString(fmt.Sprintf("%d\tRECV\t%s\t%s\t%d\n", now.UnixNano(), packetUUID, targetIP, rtt.Nanoseconds()))
	if err != nil {
		log.Errorf("unable to mark packet with UUID %s as received: %s", packetUUID, err)
	}
	err = p.Outfile.Sync()
	if err != nil {
		log.Errorf("unable to sync UDP output file: %s", err)
	}
}

func (p *UDPPinger) Run() {
	for _, target := range p.Targets {
		if target.Conn == nil {
			// Open a UDP socket
			conn, err := net.DialUDP("udp", nil, target.Addr)
			if err != nil {
				log.Errorf("unable to dial UDP target %s: %s", target.Addr.IP.String(), err)
				return
			}
			target.Conn = conn
		}

		// Capture the start time for the RTT measurement after a UDP socket is obtained
		startTime := time.Now()

		// Generate a random UUID for the packet ID
		newUUID := uuid.NewV4().String()
		payload := fmt.Sprintf("%s\t%s", newUUID, p.NodeAddr.IP.String())

		log.Infof("UDP: sending SYN against %s, UUID: %s", target.Addr.IP.String(), newUUID)
		// Send the UDP packet
		_, err := target.Conn.Write([]byte(payload))
		if err != nil {
			log.Errorf("unable to write UDP packet: %s", err)
			return
		}

		// The UUID of the packet is transfered through the uuidChan
		uuidChan := make(chan string)

		// In a goroutine, attempt to read the ACK response from the server
		var terminated bool = false
		go func(uuidChan chan string, terminated *bool) {
			var buf [1024]byte
			rlen, _, err := target.Conn.ReadFromUDP(buf[:])
			if err != nil {
				if *terminated {
					// Don't record an error if this was caused due to a timeout
					return
				}
				log.Errorf("UDP Read error %s:", err)
				_, err := p.Outfile.WriteString(fmt.Sprintf("%d\tERROR-READ\t%s\t%s\n",
					time.Now().UnixNano(), target.Addr.IP.String(), err))
				if err != nil {
					log.Errorf("Unable to output UDP Read error")
				}

				return
			}

			// Determine if the received packet is an ACK packet
			// from a request sent from this node
			payload := string(buf[0:rlen])
			payloadParts, ack := isAck(payload)
			if !ack {
				log.Errorf("Client collected non-ACK packet: %v", payload)
				return
			}
			uuidChan <- payloadParts[1]
		}(uuidChan, &terminated)

		// Block on reception of the ACK, or a timeout
		select {
		case uuid := <-uuidChan:
			p.ReceivedPacket(uuid, startTime, target.Addr.IP.String())
		case <-time.Tick(udpClientTimeout):
			// The client waits on an ACK timeout
			log.Warnf("UDP: Timeout waiting for ACK from %s", target.Addr.IP.String())
			_, err := p.Outfile.WriteString(fmt.Sprintf("%d\tLOST\t%s\t%s\n", time.Now().UnixNano(), newUUID, target.Addr.IP.String()))
			if err != nil {
				log.Errorf("unable to record UDP packet loss for uuid %s: %s", newUUID, err)
			}

			terminated = true

			err = p.Outfile.Sync()
			if err != nil {
				log.Errorf("unable to sync UDP output file: %s", err)
			}
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

func (p *UDPPinger) StartUDPServer(errChan chan<- error) {
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

		// Determine if the received packet is an ACK packet from a request sent from this node
		payload := string(udpBuffer[0:rlen])
		payloadParts, ack := isAck(payload)

		// The packet is not an acknowledgement of a packet sent from this node
		// Send an ACK packet back to the return address.
		if !ack {
			packetUUID := payloadParts[0]
			remoteIP := payloadParts[1]

			log.Infof("UDP: Received SYN, sending ACK against %s at %s:%d for packet %s",
				remoteIP, returnAddr.IP.String(), returnAddr.Port, packetUUID)

			// Prepend the payload with "ACKUDP\t"
			payload := []byte(strings.Join([]string{
				"ACKUDP",
				packetUUID,
				remoteIP,
			}, "\t"))

			_, err = sock.WriteToUDP(payload, returnAddr)
			if err != nil {
				errChan <- err
				return
			}
			time.Sleep(10 * time.Second)
			continue
		}

		// The packet is an ACK packet from a round-trip request sent by this node.
		// This should have been collected from the client socket, so report an error
		log.Errorf("UDP: Server received client-targetted ACK packet %s from %s", payload[1], payload[2])
	}
}
