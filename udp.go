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
			return
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
			return
		}
		p.SentPackets[newUUID] = PacketInfo{
			SentTime: time.Now(),
			Dest:     target,
		}

		ackChan := make(chan string)
		go func(ackChan chan string) {
			var buf [1024]byte
			rlen, _, err := conn.ReadFromUDP(buf[:])
			if err != nil {
				log.Error(err)
				return
			}

			// Determine if the received packet is an ACK packet
			// from a request sent from this node
			payload := string(buf[0:rlen])
			payloadParts, ack := isAck(payload)
			if ack {
				ackChan <- payloadParts[1]
			} else {
				log.Errorf("Client collected non-ACK packet: %v", payload)
			}
		}(ackChan)

		select {
		case uuid := <-ackChan:
			p.ReceivedPacket(uuid)
		case <-time.Tick(udpClientTimeout):
			// The client waits on an ACK timeout
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
			log.Infof("UDP: Received SYN from %s for UUID %s, sending ACK", remoteIP, packetUUID)
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
		// Inform the pinger of the received packet
		packetUUID := payloadParts[1]
		p.ReceivedPacket(packetUUID)
	}
}
