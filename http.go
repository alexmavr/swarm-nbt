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
	Targets []string
	Outfile *os.File
	client  *http.Client
}

func (p *HTTPPinger) AddURL(urlString string) {
	p.Targets = append(p.Targets, urlString)
}

func (p *HTTPPinger) Run() {
	if p.client == nil {
		p.client = &http.Client{
			Timeout: httpTimeout,
		}
	}
	for _, target := range p.Targets {
		startTime := time.Now()
		_, err := p.client.Get(target)
		if err != nil {
			// increment the HTTP timeout counter
			httpTimeouts.WithLabelValues(target).Set(1)
			continue
			log.Errorf("unable to reach http target %s: %s", target, err)
			if recordFile {
				p.Outfile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().UnixNano(), target, -1))
			}
			continue
		}
		httpTimeouts.WithLabelValues(target).Set(0)

		endTime := time.Now()
		rtt := endTime.Sub(startTime)
		log.Infof("HTTP: Target: %s receive, RTT: %v", target, rtt)
		httpRTT.WithLabelValues(target).Set(rtt.Seconds())
		if recordFile {
			_, err = p.Outfile.WriteString(fmt.Sprintf("%d\t%s\t%d\n", time.Now().UnixNano(), target, rtt.Nanoseconds()))
			if err != nil {
				log.Errorf("unable to write to HTTP results file: %s", err)
			}
		}
	}
}
