package main

import (
	"io/ioutil"

	log "github.com/Sirupsen/logrus"
)

func ProcessResults() error {
	files, err := ioutil.ReadDir("/results")
	if err != nil {
		return nil
	}

	for _, file := range files {
		log.Info(file.Name())
	}

	return nil
}
