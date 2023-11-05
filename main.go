package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime"

	"golang.org/x/crypto/ssh"
)

var (
	Version     string // this variable is defined in Makefile
	Commit      string // this variable is defined in Makefile
	Branch      string // this variable is defined in Makefile
	BuildNumber string //nolint:revive // this variable is defined in Makefile
)

var (
	helpFlag     bool
	versionFlag  bool
	username     string
	passphrase   string
	identityFile string
	localAddr    string
	remoteAddr   string
	destAddr     string
)

func main() {
	parseCommandLine()

	key, err := os.ReadFile(identityFile)
	if err != nil {
		fmt.Printf("unable to read identity file (%s): %v\n", identityFile, err)
		os.Exit(1)
	}

	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(key)
	}
	if err != nil {
		fmt.Println("unable to decode private key")
		os.Exit(1)
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.HostKeyCallback(func(string, net.Addr, ssh.PublicKey) error { return nil }),
	}

	var localListener net.Listener
	localListener, err = net.Listen("tcp", localAddr)
	if err != nil {
		fmt.Printf("failed to create local tunnel entrance (%s): %v", localAddr, err)
		os.Exit(1)
	}

	log.Printf("auto-ssh established on %s\n", localAddr)
	var localConn net.Conn
	for {
		// Setup localConn (type net.Conn)
		localConn, err = localListener.Accept()
		if err != nil {
			log.Fatalf("listen.Accept failed: %v", err)
		}
		go forward(localConn, config)
	}
}

func forward(localConn net.Conn, config *ssh.ClientConfig) {
	// connect to the remote host
	bClient, err := ssh.Dial("tcp", remoteAddr, config)
	if err != nil {
		fmt.Printf("failed to call remote address: %v", err)
		os.Exit(1)
	}

	// Setup sshConn (type net.Conn)
	sshConn, err := bClient.Dial("tcp", destAddr)
	if err != nil {
		fmt.Printf("failed to call destination address: %v", err)
		os.Exit(1)
	}

	// Copy localConn.Reader to sshConn.Writer
	go func() {
		_, err = io.Copy(sshConn, localConn)
		if err != nil {
			fmt.Printf("io.Copy failed: %v", err)
			os.Exit(1)
		}
	}()

	// Copy sshConn.Reader to localConn.Writer
	go func() {
		_, err = io.Copy(localConn, sshConn)
		if err != nil {
			fmt.Printf("io.Copy failed: %v", err)
			os.Exit(1)
		}
	}()
}

func parseCommandLine() {
	for index := 1; index < len(os.Args); index++ {
		switch os.Args[index] {
		case "-h", "--help":
			helpFlag = true
		case "-v", "--version":
			versionFlag = true
		case "-u", "--username":
			index++
			username = parameter(index)
		case "-i", "--identity":
			index++
			identityFile = parameter(index)
		case "-l", "--local":
			index++
			localAddr = parameter(index)
		case "-r", "--remote":
			index++
			remoteAddr = parameter(index)
		case "-d", "--destination":
			index++
			destAddr = parameter(index)
		}
	}

	if helpFlag {
		help()
	}
	if versionFlag {
		version()
	}
}

func parameter(index int) string {
	if index < len(os.Args) {
		return os.Args[index]
	}
	fmt.Printf("%s requires a value\n", os.Args)
	os.Exit(1)
	return ""
}

func help() {
	fmt.Println("Automatic tunneling on demand")
	fmt.Println("Usage:")
	fmt.Println("  -h, --help        Display this message")
	fmt.Println("  -u, --username    Remote username")
	fmt.Println("  -i, --identity    Private key for authentication")
	fmt.Println("  -p, --passphrase  Private key decryption password")
	fmt.Println("  -l, --local       Local tunnel entrance address  (\"<ip>:<port>\")")
	fmt.Println("  -r, --remote      Remote tunnel exit address     (\"<ip>:<port>\")")
	fmt.Println("  -d, --destination Destination server address     (\"<ip>:<port>\")")
	fmt.Println("  -v, --version     Display version information")
	os.Exit(0)
}

func version() {
	fmt.Printf("%s verison %s %s/%s, build %s, commit %s\n", os.Args[0], Version, runtime.GOOS, runtime.GOARCH, BuildNumber, Commit)
	os.Exit(0)
}
