package main

import "github.com/prometheus/client_golang/prometheus"

var (
	icmpRTT = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "icmp_rtt_gauge",
		Help: "ICMP Round-Trip delay time gauge",
	})
	udpRTT = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "udp_rtt_gauge",
		Help: "UDP Round-Trip delay time gauge",
	})
	udpPacketLoss = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "udp_packet_loss_counter",
		Help: "UDP Packet loss counter",
	})
	httpRTT = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_rtt_gauge",
		Help: "HTTP Round-Trip delay time gauge",
	})
)

func init() {
	prometheus.MustRegister(icmpRTT)
	prometheus.MustRegister(udpRTT)
	prometheus.MustRegister(udpPacketLoss)
	prometheus.MustRegister(httpRTT)
}
