Docker Swarm-Mode Network Benchmark Tool
=======================================

This tool measures the network quality of service across all nodes in a Swarm by capturing the following metrics over an extended time period:
- UDP and TCP packet loss rate and round-trip delay time for all links
- Percentage of Docker Engine Gossip traffic per link
- Network Partition & Merge transient times

Individual measurements will be stored on a local volume on each node. When the benchmark operation is stopped,
these measurements will be gathered on the tool runner container and processed into final results

Usage
=====

The tool supports the following operations:

* start/continue benchmark:
	```
		docker run --rm -v /var/run/docker.sock:/var/run/docker.sock alexmavr/swarm-netbench start
	```

* pause benchmark:
	```
		docker run --rm -v /var/run/docker.sock:/var/run/docker.sock alexmavr/swarm-netbench pause
	```

* stop & process results
	```
		docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v /path/to/results/dir:/output alexmavr/swarm-netbench stop  
	```
