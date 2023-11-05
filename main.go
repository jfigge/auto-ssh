package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	GOOS_Linux   = "linux"
	GOOS_Darwin  = "darwin"
	GOOS_Windows = "windows"
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
	verboseFlag  int
	hostKeyFile  string
	username     string
	passphrase   string
	identityFile string
	localAddr    string
	remoteAddr   string
	forwardAddr  string
)

func main() {
	defaultValues()
	parseCommandLine()
	validateCommandLine()

	key, err := os.ReadFile(identityFile)
	if os.IsPermission(err) {
		fmt.Printf("  Error - identity file (%s) cannot be read: permission denied\n", identityFile)
		os.Exit(1)
	} else if err != nil {
		fmt.Printf("  Error - identity file (%s) cannot be read: %v\n", identityFile, err)
		os.Exit(1)
	}

	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(key)
	}
	if err != nil {
		fmt.Printf("  Error - identity file (%s) cannot be decode: %v\n", identityFile, err)
		os.Exit(1)
	}

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.HostKeyCallback(hostKeyCallbackProvider),
	}

	var localListener net.Listener
	localListener, err = net.Listen("tcp", localAddr)
	if err != nil {
		fmt.Printf("  Error - local tunnel entrance (%s) cannot be created: %v\n", localAddr, err)
		os.Exit(1)
	}

	bClient := remoteConnect(config)
	bClient.Close()

	if verboseFlag > 0 {
		fmt.Printf("  Status - auto-ssh listening on %s\n", localAddr)
	}

	var localConn net.Conn
	for {
		// Setup localConn (type net.Conn)
		localConn, err = localListener.Accept()
		if err != nil {
			fmt.Printf("  Error - listener accept failed: %v\v", err)
			os.Exit(1)
		}
		if verboseFlag > 0 {
			fmt.Printf("  Status - connection accepted %s\n", localConn.RemoteAddr())
		}
		go forward(localConn, bClient, config)
	}
}

func forward(localConn net.Conn, bClient *ssh.Client, config *ssh.ClientConfig) {
	if verboseFlag > 1 {
		fmt.Printf("  Status - conneting to forward server %s\n", forwardAddr)
	}
	sshConn, err := bClient.Dial("tcp", forwardAddr)
	if err != nil {
		bClient = remoteConnect(config)
		sshConn, err = bClient.Dial("tcp", forwardAddr)
		if err != nil {
			fmt.Printf("  Error - failed to call destination address: %v\n", err)
			os.Exit(1)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	// Copy localConn.Reader to sshConn.Writer
	go func() {
		defer wg.Done()
		_, err1 := io.Copy(sshConn, localConn)
		if err1 != nil {
			fmt.Printf("  Error - failed to transmit request: %v\n", err1)
			os.Exit(1)
		}
	}()

	// Copy sshConn.Reader to localConn.Writer
	go func() {
		defer wg.Done()
		_, err2 := io.Copy(localConn, sshConn)
		if err2 != nil {
			fmt.Printf(" Error - failed to receive response: %v\n", err2)
			os.Exit(1)
		}
	}()

	wg.Wait()

	if verboseFlag > 0 {
		fmt.Printf("  Status - closing connection %s\n", localConn.RemoteAddr())
	}
	localConn.Close()
}

func remoteConnect(config *ssh.ClientConfig) *ssh.Client {
	if verboseFlag > 0 {
		fmt.Printf("  Status - conneting to remote server %s\n", remoteAddr)
	}

	bClient, err := ssh.Dial("tcp", remoteAddr, config)
	if err != nil {
		fmt.Printf("  Error - failed to connect to remote address: %v\n", err)
		os.Exit(1)
	}
	return bClient
}

func defaultValues() {
	currentUser, err := user.Current()
	if err != nil {
		fmt.Printf("  Error - failed to lookup current user: %v\n", err)
		os.Exit(1)
	}
	username = currentUser.Username

	switch runtime.GOOS {
	case GOOS_Linux:
		hostKeyFile = fmt.Sprintf("/home/%s/.ssh/known_hosts", username)
	case GOOS_Darwin:
		hostKeyFile = fmt.Sprintf("/Users/%s/.ssh/known_hosts", username)
	case GOOS_Windows:
		hostKeyFile = fmt.Sprintf("C:\\Users\\%s\\.ssh\\known_hosts", username)
	default:
		fmt.Printf("  Error - unsupported OS type: %s\n", runtime.GOOS)
		os.Exit(1)
	}

}

func parseCommandLine() {
	for index := 1; index < len(os.Args); index++ {
		switch os.Args[index] {
		case "-h", "--help":
			helpFlag = true
		case "-V", "--version":
			versionFlag = true
		case "-v", "--verbose":
			verboseFlag = 1
		case "-vv", "--very-verbose":
			verboseFlag = 2
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
		case "-f", "--forward":
			index++
			forwardAddr = parameter(index)
		case "-H", "--hostkey-file":
			index++
			hostKeyFile = parameter(index)
		default:
			if strings.HasPrefix(os.Args[index], "-") {
				fmt.Printf("  Error - unknown paramters (%s) at position %d\n", os.Args[index], index)
			} else {
				fmt.Printf("  Error - unexpected argument (%s) as position %d\n", os.Args[index], index)
			}
			helpFlag = true
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
	if index < len(os.Args) && !strings.HasPrefix(os.Args[index], "-") {
		return os.Args[index]
	}
	fmt.Printf("  Error - paramreter %s requires a value\n", os.Args[index-1])
	os.Exit(1)
	return ""
}

func validateCommandLine() {
	fail := false
	// Check username
	if username == "" {
		fmt.Printf("  Error - missing renmote logon username, defaulting to .  Use -l <ip>:<port>\n")
		fail = true
	}

	if validateAddress(localAddr, "local", "-l") {
		fail = true
	}

	if validateAddress(remoteAddr, "remote", "-r") {
		fail = true
	}

	if validateAddress(forwardAddr, "forward", "-f") {
		fail = true
	}

	if hostKeyFile != "" {
		if fi, err := os.Stat(hostKeyFile); os.IsNotExist(err) {
			fmt.Printf("  Error - hostkey file (%s) cannot be read: file not found\n", hostKeyFile)
			fail = true
		} else if fi.IsDir() {
			fmt.Printf("  Error - hostkey file (%s) cannot be read: file is a directory\n", hostKeyFile)
			fail = true
		} else if _, err = knownhosts.New(hostKeyFile); os.IsPermission(err) {
			fmt.Printf("  Error - hostkey file (%s) cannot be read: permission denied\n", hostKeyFile)
			fail = true
		} else if err != nil {
			fmt.Printf("  Error - hostkey file (%s) cannot be read: %v\n", hostKeyFile, err)
			fail = true
		}
	}

	if identityFile == "" {
		fmt.Printf("  Error - missing identity file.  Use -i\n")
		fail = true
	} else if fi, err := os.Stat(identityFile); os.IsNotExist(err) {
		fmt.Printf("  Error - identity file (%s) cannot be read: file not found\n", identityFile)
		fail = true
	} else if fi.IsDir() {
		fmt.Printf("  Error - identity file (%s) cannot be read: file is a directory\n", identityFile)
		fail = true
	}

	if fail {
		os.Exit(1)
	}
}

func validateAddress(addr, name, parameter string) bool {
	if addr == "" {
		fmt.Printf("  Error - missing %s address.  Use %s <ip>:<port>\n", name, parameter)
		return true
	}
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

func help() {
	fmt.Printf("Automatic tunneling on demand\n")
	fmt.Printf("Usage:\n")
	fmt.Printf("  -h, --help         Display this message.\n")
	fmt.Printf("  -u, --username     Remote username.  Defaults to %s.\n", username)
	fmt.Printf("  -i, --identity     Private key for authentication.\n")
	fmt.Printf("  -p, --passphrase   Private key decryption password.\n")
	fmt.Printf("  -h, --hostkey-file Specify the known-hosts file. Defaulted. Disable using: -H \"\"\n")
	fmt.Printf("  -l, --local        Local tunnel bind address: (<address>:<port>)\n")
	fmt.Printf("  -r, --remote       Remote ssh server address: (<address>:<port>)\n")
	fmt.Printf("  -f, --forward      Forwarding server address: (<address>:<port>)\n")
	fmt.Printf("  -v, --verbose      Verbose mode.  Prints progress debug messages.\n")
	fmt.Printf("  -V, --version      Display version information.\n")
	os.Exit(0)
}

func version() {
	fmt.Printf("%s verison %s %s/%s, build %s, commit %s\n", os.Args[0], Version, runtime.GOOS, runtime.GOARCH, BuildNumber, Commit)
	os.Exit(0)
}

func hostKeyCallbackProvider(addr string, na net.Addr, pk ssh.PublicKey) error {
	if hostKeyFile == "" {
		return nil
	}
	hostKeyCallback, err := knownhosts.New(hostKeyFile)
	if err != nil {
		return fmt.Errorf("failed to access hostkey file (%s): %v", hostKeyFile, err)
	}
	return hostKeyCallback(addr, na, pk)
}
