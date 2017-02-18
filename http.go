package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
)

// HTTPPinger
type HTTPPinger struct {
	Targets   []*HTTPTarget
	Outfile   *os.File
	client    *http.Client
	IsManager bool
}

type HTTPTarget struct {
	URL       string
	IsManager bool
}

func (p *HTTPPinger) AddTarget(node *Node) {
	url := fmt.Sprintf("http://%s:%d", node.Address, httpServerPort)
	p.Targets = append(p.Targets, &HTTPTarget{
		URL:       url,
		IsManager: node.IsManager,
	})
}

func (p *HTTPPinger) Run() {
	if p.client == nil {
		p.client = &http.Client{
			Timeout: httpTimeout,
		}
	}
	for _, target := range p.Targets {
		startTime := time.Now()
		_, err := p.client.Get(target.URL)
		if err != nil {
			// increment the HTTP timeout counter
			httpTimeouts.WithLabelValues(target.URL, formatManagersLabel(p.IsManager, target.IsManager)).Set(1)
			httpRTT.WithLabelValues(target.URL, formatManagersLabel(p.IsManager, target.IsManager)).Set(0)
			continue
			log.Errorf("unable to reach http target %s: %s", target, err)
			if recordFile {
				p.Outfile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().UnixNano(), target, -1))
			}
			continue
		}
		httpTimeouts.WithLabelValues(target.URL, formatManagersLabel(p.IsManager, target.IsManager)).Set(0)

		endTime := time.Now()
		rtt := endTime.Sub(startTime)
		log.Infof("HTTP: Target: %s receive, RTT: %v", target.URL, rtt)
		httpRTT.WithLabelValues(target.URL, formatManagersLabel(p.IsManager, target.IsManager)).Set(rtt.Seconds())
		if recordFile {
			_, err = p.Outfile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().UnixNano(), target.URL, rtt.Nanoseconds()))
			if err != nil {
				log.Errorf("unable to write to HTTP results file: %s", err)
			}
		}
	}
}
