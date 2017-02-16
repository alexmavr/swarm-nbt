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
		httpRTT.WithLabelValues(target).Add(rtt.Seconds())
		if err != nil {
			log.Errorf("unable to write to HTTP results file: %s", err)
		}
	}
}
