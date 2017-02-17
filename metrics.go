package main

import "github.com/prometheus/client_golang/prometheus"

var (
	icmpRTT = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "icmp_rtt_gauge_seconds",
		Help: "ICMP Round-Trip delay time gauge in seconds",
	}, []string{"target"})
	udpRTT = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "udp_rtt_gauge_seconds",
		Help: "UDP Round-Trip delay time gauge in seconds",
	}, []string{"target"})
	udpPacketLoss = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "udp_packet_loss_counter_cumulative",
		Help: "Cumulative counter of UDP packet losses",
	}, []string{"target"})
	httpRTT = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "http_rtt_gauge_seconds",
		Help: "HTTP Round-Trip delay time gauge in seconds",
	}, []string{"target"})
	httpTimeouts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "http_timeout_occurence",
		Help: "Boolean counter of HTTP session timeouts (10 second default timeout)",
	}, []string{"target"})
)

func init() {
	prometheus.MustRegister(icmpRTT)
	prometheus.MustRegister(udpRTT)
	prometheus.MustRegister(udpPacketLoss)
	prometheus.MustRegister(httpRTT)
}
