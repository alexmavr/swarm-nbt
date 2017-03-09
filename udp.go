package main

import (
	"fmt"
	"net"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	uuid "github.com/satori/go.uuid"
)

// UDPPinger
type UDPPinger struct {
	Targets   []*udpTarget
	NodeAddr  *net.UDPAddr
	IsManager bool
	Timeout   time.Duration
}

type udpTarget struct {
	IsManager bool
	Addr      *net.UDPAddr
	Conn      *net.UDPConn
}

func (p *UDPPinger) AddTarget(node *Node) {
	// It's not currently possible to respond to the client socket when communicating with
	// a server on the same container. For this reason, silently skip the same from
	// the targets list
	if node.Address == p.NodeAddr.IP.String() {
		return
	}

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", node.Address, udpServerPort))
	if err != nil {
		log.Errorf("unable to resolve UDP address: %s", node.Address)
		return
	}

	p.Targets = append(p.Targets, &udpTarget{
		IsManager: node.IsManager,
		Addr:      udpAddr,
		Conn:      nil,
	})
}

func (p *UDPPinger) ReceivedPacket(packetUUID string, startTime time.Time, target *udpTarget) {
	// Mark the packet as received
	now := time.Now()
	rtt := now.Sub(startTime)
	log.Infof("UDP: Received ACK, UUID: %s, RTT: %v", packetUUID, rtt)
	udpRTT.WithLabelValues(target.Addr.IP.String(), formatManagersLabel(p.IsManager, target.IsManager)).Set(rtt.Seconds())
}

func (p *UDPPinger) Run() {
	for _, target := range p.Targets {
		// Capture the start time for the RTT measurement after a UDP socket is obtained
		startTime := time.Now()

		// Open a UDP socket
		conn, err := net.DialUDP("udp", nil, target.Addr)
		if err != nil {
			log.Errorf("unable to dial UDP target %s: %s", target.Addr.IP.String(), err)
			return
		}
		target.Conn = conn
		defer target.Conn.Close()

		// Generate a random UUID for the packet ID
		newUUID := uuid.NewV4().String()
		payload := fmt.Sprintf("%s\t%s", newUUID, p.NodeAddr.IP.String())

		log.Infof("UDP: sending SYN against %s, UUID: %s", target.Addr.IP.String(), newUUID)
		// Send the UDP packet
		_, err = target.Conn.Write([]byte(payload))
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
				udpPacketLoss.WithLabelValues(target.Addr.IP.String(), formatManagersLabel(p.IsManager, target.IsManager)).Set(1)
				udpRTT.WithLabelValues(target.Addr.IP.String(), formatManagersLabel(p.IsManager, target.IsManager)).Set(0)

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
			p.ReceivedPacket(uuid, startTime, target)
			udpPacketLoss.WithLabelValues(target.Addr.IP.String(), formatManagersLabel(p.IsManager, target.IsManager)).Set(0)
		case <-time.Tick(udpClientTimeout):
			// The client waits on an ACK timeout
			log.Warnf("UDP: Timeout waiting for ACK from %s", target.Addr.IP.String())
			udpPacketLoss.WithLabelValues(target.Addr.IP.String(), formatManagersLabel(p.IsManager, target.IsManager)).Set(1)
			udpRTT.WithLabelValues(target.Addr.IP.String(), formatManagersLabel(p.IsManager, target.IsManager)).Set(0)
			terminated = true
		}
		target.Conn.Close()
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
	sock.SetReadBuffer(256)

	var udpBuffer [256]byte
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
			continue
		}

		// The packet is an ACK packet from a round-trip request sent by this node.
		// This should have been collected from the client socket, so report an error
		log.Errorf("UDP: Server received client-targetted ACK packet %s from %s", payload[1], payload[2])
	}
}
