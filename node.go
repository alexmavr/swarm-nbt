package main

type Node struct {
	Hostname  string `json:"hostname"`
	Address   string `json:"ip"`
	IsManager bool   `json:is_manager"`
}
