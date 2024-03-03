package net

import (
	"fmt"
	"math/rand"
	"net"
)

func GetAvailablePort() (int, error) {
	// The range of ports to check for availability
	minPort := 1024
	maxPort := 65535

	for i := 0; i < 1000; i++ {
		port := rand.Intn(maxPort-minPort) + minPort
		if !isPortOpen(port) {
			return port, nil
		}
	}

	return 0, fmt.Errorf("could not find an available port")
}

func isPortOpen(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return true // Port is already in use
	}
	defer listener.Close()
	return false // Port is available
}
