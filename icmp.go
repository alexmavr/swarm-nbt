package main

import (
	"net"
	"time"

	log "github.com/Sirupsen/logrus"
	fastping "github.com/tatsushid/go-fastping"
)

type ICMPPinger struct {
	pinger          *fastping.Pinger
	IsManager       bool
	targetIsManager map[string]bool
	initialized     bool
}

func (p *ICMPPinger) Init() {
	p.initialized = true
	if p.pinger == nil {
		p.pinger = fastping.NewPinger()
	}
	p.pinger.MaxRTT = icmpMaxRTT
	p.pinger.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
		log.Infof("ICMP: Target: %s receive, RTT: %v", addr.String(), rtt)
		targetManager, ok := p.targetIsManager[addr.IP.String()]
		if !ok {
			log.Errorf("unable to determine target manager status for IP %s", addr.IP.String())
			targetManager = false // better to have wrong labels than no data

		}
		icmpManagerLabels := formatManagersLabel(p.IsManager, targetManager)
		icmpRTT.WithLabelValues(addr.IP.String(), icmpManagerLabels).Set(rtt.Seconds())
	}
	p.pinger.OnIdle = func() {
		log.Debugf("ICMP Pinger Idle")
	}
}

func (p *ICMPPinger) AddTarget(node *Node) {
	if p.pinger == nil {
		p.pinger = fastping.NewPinger()
	}

	if p.targetIsManager == nil {
		p.targetIsManager = make(map[string]bool)
	}

	ra, err := net.ResolveIPAddr("ip4:icmp", node.Address)
	if err != nil {
		log.Errorf("unable to resolve IPv4 address %s for ICMP: %s", node.Address, err)
	}

	p.pinger.AddIPAddr(ra)
	p.targetIsManager[ra.IP.String()] = node.IsManager
}

func (p *ICMPPinger) Run() error {
	if !p.initialized {
		p.Init()
	}
	return p.pinger.Run()
}
