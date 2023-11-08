package main

import (
	"fmt"
	"net"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"
)

var (
	hostLock = sync.Mutex{}
	hosts    = map[string]*Host{}
)

type Host struct {
	remoteAddr string
	config     *ssh.ClientConfig
	client     *ssh.Client
	lock       sync.Mutex
}

func (h *Host) connect() *ssh.Client {
	if verboseFlag > 1 {
		fmt.Printf(" Status - conneting to remote server %s\n", h.remoteAddr)
	}

	bClient, err := ssh.Dial("tcp", h.remoteAddr, h.config)
	if err != nil {
		fmt.Printf("  Error - failed to connect to remote address: %v\n", err)
		os.Exit(1)
	}
	return bClient
}

func (h *Host) dial(n, addr string) net.Conn {
	h.lock.Lock()
	defer h.lock.Unlock()
	conn, err := h.client.Dial(n, addr)
	if err != nil {
		if verboseFlag > 1 {
			fmt.Printf("  Error - failed to dial remote address: %v\n", err)
		}
		h.client = h.connect()
		conn, err = h.client.Dial("tcp", addr)
		if err != nil {
			fmt.Printf("  Error - failed to call remote address: %v\n", err)
			os.Exit(1)
		}
	}
	return conn
}
