package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type Tunnel struct {
	localAddr   string
	remoteAddr  string
	forwardAddr string
	host        *Host
}

var connection = atomic.Int32{}
var connections = atomic.Int32{}

func NewTunnel(tunnelMap string) *Tunnel {
	parts := strings.Split(tunnelMap, ":")
	localAddress := ""
	remoteAddress := ""
	forwardAddress := ""

	switch len(parts) {
	case 6:
		localAddress = fmt.Sprintf("%s:%s", parts[0], parts[1])
		remoteAddress = fmt.Sprintf("%s:%s", parts[2], parts[3])
		forwardAddress = fmt.Sprintf("%s:%s", parts[4], parts[5])
	case 5:
		localAddress = fmt.Sprintf("localhost:%s", parts[0])
		remoteAddress = fmt.Sprintf("%s:%s", parts[1], parts[2])
		forwardAddress = fmt.Sprintf("%s:%s", parts[3], parts[4])
	default:
		fmt.Printf("  Error - invalid syntax for tunnel")
		help()
	}

	if !validateTunnel(localAddress, remoteAddress, forwardAddress) {
		return nil
	}

	return &Tunnel{
		localAddr:   localAddress,
		remoteAddr:  remoteAddress,
		forwardAddr: forwardAddress,
	}
}

func validateTunnel(localAddr, remoteAddr, forwardAddr string) bool {
	valid := true
	if validateAddress(localAddr, "local") {
		valid = false
	}

	if validateAddress(remoteAddr, "remote") {
		valid = false
	}

	if validateAddress(forwardAddr, "forward") {
		valid = false
	}
	return valid
}

func validateAddress(addr, name string) bool {
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		fmt.Printf("  Error - invalid %s address.  Required <ip address>:<port>\n", name)
		return true
	}
	fail := true
	ips, _ := net.LookupIP(parts[0])
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			fail = false
		}
	}
	if fail {
		fmt.Printf("  Error - invalid %s address: cannot resolve %s\n", name, addr)
	}

	if i, err := strconv.Atoi(parts[1]); err != nil {
		fmt.Printf("  Error - invalid %s port - %v\n", name, err.Error())
		fail = true
	} else if i < 1 || i > 65536 {
		fmt.Printf("  Error - invalid %s port out of range - %d must be between 1 and 65536\n", name, i)
		fail = true
	}
	return fail
}

func (t *Tunnel) open(signer ssh.Signer) {
	t.host = createHost(t.remoteAddr, signer)
	localListener, err := net.Listen("tcp", t.localAddr)
	if err != nil {
		fmt.Printf("  Error - local tunnel entrance (%s) cannot be created: %v\n", t.localAddr, err)
		os.Exit(1)
	}

	if verboseFlag > 0 {
		fmt.Printf(" Status - auto-ssh listening on %s\n", t.localAddr)
	}

	for {
		var localConn net.Conn
		localConn, err = localListener.Accept()
		if err != nil {
			fmt.Printf("  Error - listener accept failed: %v\v", err)
			os.Exit(1)
		}
		if verboseFlag > 0 {
			fmt.Printf(" Status - connection accepted %s\n", localConn.RemoteAddr())
		}
		go t.forward(localConn)
	}
}

func createHost(remoteAddr string, signer ssh.Signer) *Host {
	hostLock.Lock()
	defer hostLock.Unlock()
	host := hosts[remoteAddr]
	if host == nil {
		host = &Host{
			config: &ssh.ClientConfig{
				User: username,
				Auth: []ssh.AuthMethod{
					ssh.PublicKeys(signer),
				},
				HostKeyCallback: hostKeyCallbackProvider,
			},
			remoteAddr: remoteAddr,
			lock:       sync.Mutex{},
		}
		hosts[remoteAddr] = host
		host.client = host.connect()
	}
	return host
}

func (t *Tunnel) forward(localConn net.Conn) {
	connection.Add(1)
	id := connection.Load()

	if verboseFlag > 0 {
		fmt.Printf(" Status - id:%d conneting to forward server %s\n", id, t.forwardAddr)
	}
	sshConn := t.host.dial("tcp", t.forwardAddr)
	wg := sync.WaitGroup{}
	wg.Add(2)

	ctx, cancel := context.WithCancel(context.Background())
	closer := func() {
		autoClose(ctx, sshConn, localConn, id)
	}

	connected1 := true
	connected2 := true
	// Copy localConn.Reader to sshConn.Writer
	go func() {
		connections.Add(1)
		defer wg.Done()
		_, err1 := io.Copy(sshConn, localConn)
		connected1 = false
		connections.Add(-1)
		if verboseFlag > 1 {
			fmt.Printf(" Status - id:%d c:%d closed1\n", id, connections.Load())
		}
		if err1 != nil {
			fmt.Printf("  Error - failed to transmit request: %v\n", err1)
			os.Exit(1)
		}
		if connected2 {
			go closer()
		}
	}()

	// Copy sshConn.Reader to localConn.Writer
	go func() {
		connections.Add(1)
		defer wg.Done()
		_, err2 := io.Copy(localConn, sshConn)
		connected2 = false
		connections.Add(-1)
		if verboseFlag > 1 {
			fmt.Printf(" Status - id:%d c:%d closed2\n", id, connections.Load())
		}
		if err2 != nil {
			fmt.Printf("  Error - failed to receive response: %v\n", err2)
			os.Exit(1)
		}
		if connected1 {
			go closer()
		}
	}()

	wg.Wait()
	cancel()
	if verboseFlag == 1 {
		fmt.Printf(" Status - id:%d closing connection %s\n", id, localConn.RemoteAddr())
	} else if verboseFlag > 1 {
		fmt.Printf(" Status - id:%d c:%d closing connection %s\n", id, connections.Load(), localConn.RemoteAddr())
	}
}

func autoClose(ctx context.Context, conn net.Conn, conn2 net.Conn, id int32) {
	status := "terminated"
	if verboseFlag > 1 {
		fmt.Printf(" Status - id:%d c:%d auto-closer initiated\n", id, connections.Load())
	}
	t := time.NewTimer(30 * time.Second)
	select {
	case <-t.C:
		status = "triggered"
	case <-ctx.Done():
	}
	if conn != nil {
		_ = conn.Close()
	}
	if conn2 != nil {
		_ = conn2.Close()
	}
	if verboseFlag > 1 {
		fmt.Printf(" Status - id:%d c:%d auto-closer %s\n", id, connections.Load(), status)
	}
}
