/*
 * Copyright (C) 2024 by Jason Figge
 */

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"us.figge.auto-ssh/internal/core/config"
	engineModels "us.figge.auto-ssh/internal/resources/models"
)

var (
	errInvalidWrite = errors.New("invalid write result")
)

type tunnelData struct {
	*config.Tunnel
	lock   sync.Mutex
	host   engineModels.HostInternal
	conns  []net.Conn
	stats  engineModels.Stats
	cancel context.CancelFunc
	wg     *sync.WaitGroup
}

type Entry struct {
	appCtx context.Context
	*tunnelData
}

func (t *Entry) init(ctx context.Context, stats engineModels.Stats, wg *sync.WaitGroup) {
	t.appCtx = ctx
	t.stats = stats
	t.wg = wg
}

func (t *Entry) Start() {
	if t.Status.Running != "Stopped" {
		return
	}
	t.Status.Running = "Starting"
	var ctx context.Context
	ctx, t.cancel = context.WithCancel(t.appCtx)
	localListener, err := net.Listen("tcp", t.Local().String())
	if err != nil {
		fmt.Printf("  Error - tunnel (%s) entrance (%s) cannot be created: %v\n", t.Name(), t.Local().String(), err)
		return
	}
	fmt.Printf("  Info  - tunnel (%s) entrance opened at %s\n", t.Name(), t.Local().String())
	t.wg.Add(1)
	go t.waitForTermination(ctx, localListener)
	go t.runningAcceptLoop(ctx, localListener)
	t.Status.Running = "Started"
}

func (t *Entry) Stop() {
	if t.cancel != nil {
		t.Status.Running = "Stopping"
		t.cancel()
	}
}

func (t *Entry) runningAcceptLoop(ctx context.Context, localListener net.Listener) {
	defer func() {
		t.Status.Running = "Stopped"
		t.wg.Done()
	}()
	for {
		localConn, err := localListener.Accept()
		if err != nil {
			var opErr *net.OpError
			if errors.As(err, &opErr) && opErr.Op == "accept" && opErr.Err.Error() == "use of closed network connection" {
				// Close quietly and we're likely shutting down
				return
			}
			fmt.Printf("  Error - tunnel (%s) listener accept failed: %v\n", t.Name(), err)
			return
		}
		fmt.Printf("  Info  - Connected tunnel: %v\n", t.Name())
		go t.forward(ctx, localConn)
	}
}

func (t *Entry) forward(ctx context.Context, localConn net.Conn) {
	id := t.addConnection(localConn)
	defer t.removeConnection(localConn)
	if config.VerboseFlag {
		fmt.Printf("  Info  - tunnel (%s) id:%s conneting to forward server %s\n", t.Name(), t.Id(), t.Remote().String())
	}

	var sshConn net.Conn
	if t.host != nil {
		if !t.host.(engineModels.HostInternal).Open() {
			// TODO Failed to connect
			return
		}
		var ok bool
		sshConn, ok = t.host.(engineModels.HostInternal).Dial(t.Remote().String())
		if !ok {
			// TODO failed to connect
			return
		}
	} else {
		// Direct forward
		var err error
		sshConn, err = net.Dial("tcp", t.Remote().String())
		if err != nil {
			fmt.Printf("  Error - tunnel (%s) id:%d unable to forward to server %s\n", t.Name(), id, t.Remote().String())
			return
		}
	}
	NewTunnelConnection(t.Name(), t.Id(), t.stats, sshConn, localConn).Start(ctx)
}

func (t *Entry) Validate(he engineModels.HostEngineInternal) bool {
	t.tunnelData.Name = strings.TrimSpace(t.tunnelData.Name)
	if t.tunnelData.Name == "" {
		fmt.Printf("  Error - tunnel name cannot be blank\n")
		t.Status.Valid = false
	}
	if t.tunnelData.Remote == nil || t.tunnelData.Remote.IsBlank() {
		fmt.Printf("  Error - tunnel (%s) requires a forward address\n", t.tunnelData.Name)
		t.Status.Valid = false
	} else if !t.tunnelData.Remote.Validate("tunnel", t.tunnelData.Name, "forward address", true, false) {
		t.Status.Valid = false
	}

	if (t.tunnelData.Local == nil || t.tunnelData.Local.IsBlank()) && t.tunnelData.Remote != nil && t.tunnelData.Remote.IsValid() {
		fmt.Printf("  Warn  - tunnel (%s) Local entrance undefined. Defaulting to 127.0.0.1:%d\n", t.tunnelData.Name, t.tunnelData.Remote.Port())
		t.tunnelData.Local = config.NewAddress(fmt.Sprintf("127.0.0.1:%d", t.tunnelData.Remote.Port()))
	}
	if t.tunnelData.Local == nil || t.tunnelData.Local.IsBlank() {
		fmt.Printf("  Error - tunnel (%s) missing a local address that cannot be derived\n", t.tunnelData.Name)
	} else if !t.tunnelData.Local.Validate("tunnel", t.tunnelData.Name, "local address", true, false) {
		t.Status.Valid = false
	}

	t.tunnelData.Host = strings.TrimSpace(t.tunnelData.Host)
	if t.tunnelData.Host == "" {
		fmt.Printf("  Info  - tunnel (%s) exits on the local host\n", t.tunnelData.Name)
	} else if host, ok := he.Host(t.tunnelData.Host); !ok {
		fmt.Printf("  Error - tunnel (%s) remote host (%s) undefined\n", t.tunnelData.Name, t.tunnelData.Host)
		t.Status.Valid = false
	} else if !host.Valid() {
		fmt.Printf("  Error - tunnel (%s) remote host (%s) is invalid\n", t.tunnelData.Name, t.tunnelData.Host)
		t.Status.Valid = false
	} else if t.Status.Valid {
		t.host = host.(engineModels.HostInternal)
		t.host.Referenced()
	}

	if config.VerboseFlag && t.Status.Valid {
		fmt.Printf("  Info  - tunnel (%s) validated\n", t.tunnelData.Name)
	}

	//t.stats = &TunnelStats{
	//	Name: t.Name,
	//	Port: t.Local.Port(),
	//}

	return t.Status.Valid
}

func (t *Entry) Id() string {
	return t.tunnelData.Id
}
func (t *Entry) Name() string {
	return t.tunnelData.Name
}
func (t *Entry) Local() *config.Address {
	return t.tunnelData.Local
}
func (t *Entry) Remote() *config.Address {
	return t.tunnelData.Remote
}
func (t *Entry) Host() string {
	return t.tunnelData.Host
}
func (t *Entry) Valid() bool {
	return t.tunnelData.Status.Valid
}
func (t *Entry) Running() string {
	return t.tunnelData.Status.Running
}
func (t *Entry) Metadata() *config.Metadata {
	return t.tunnelData.Metadata
}

func (t *Entry) waitForTermination(ctx context.Context, localListener net.Listener) {
	<-ctx.Done()
	fmt.Printf("  Info  - tunnel (%s) stopped listening on %s\n", t.Name(), t.Local().String())
	_ = localListener.Close()
	t.lock.Lock()
	defer t.lock.Unlock()
	for _, conn := range t.conns {
		_ = conn.Close()
	}
	t.conns = []net.Conn{}
	t.cancel = nil
}

func (t *Entry) addConnection(conn net.Conn) int {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.conns = append(t.conns, conn)
	return t.stats.Connected()
}

func (t *Entry) removeConnection(conn net.Conn) {
	t.lock.Lock()
	defer t.lock.Unlock()
	conns := make([]net.Conn, 0, len(t.conns)-1)
	for _, c := range t.conns {
		if conn != c {
			conns = append(conns, c)
		}
	}
	_ = conn.Close()
	t.stats.Disconnected()
	t.conns = conns
}
