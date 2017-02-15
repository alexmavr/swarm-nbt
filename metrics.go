package main

import "github.com/prometheus/client_golang/prometheus"

var (
	icmpRTT = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "icmp_rtt_histogram",
	})
	udpRTT = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "udp_rtt_histogram",
	})
	udpPacketLoss = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "udp_packet_loss_histogram",
	})
	httpRTT = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "http_rtt_histogram",
	})
)

func init() {
	prometheus.MustRegister(icmpRTT)
	prometheus.MustRegister(udpRTT)
	prometheus.MustRegister(udpPacketLoss)
	prometheus.MustRegister(httpRTT)
}
