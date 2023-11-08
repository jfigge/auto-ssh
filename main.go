package main

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"runtime"
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
	tunnels      []*Tunnel
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

	wg := sync.WaitGroup{}
	for i := range tunnels {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			tunnels[index].open(signer)

		}(i)
	}
	wg.Wait()
	if verboseFlag > 0 {
		fmt.Printf("  Status - All tunnels closed.  Stopped\n")
	}
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
		case "-vvv", "--very-very-verbose":
			verboseFlag = 3
		case "-u", "--username":
			index++
			username = parameter(index)
		case "-i", "--identity":
			index++
			identityFile = parameter(index)
		case "-t":
			index++
			tunnelMap := parameter(index)
			tunnel := NewTunnel(tunnelMap)
			if tunnel != nil {
				tunnels = append(tunnels, tunnel)
			}
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

	if len(tunnels) == 0 {
		fmt.Printf("  Error - no valid tunnels defined.  Use -t\n")
		fail = true
	}

	if fail {
		os.Exit(1)
	}
}

func help() {
	fmt.Printf("Automatic tunneling on demand\n")
	fmt.Printf("Usage:\n")
	fmt.Printf("  -h, --help          Display this message.\n")
	fmt.Printf("  -u, --username      Remote username.  Defaults to %s.\n", username)
	fmt.Printf("  -i, --identity      Private key for authentication.\n")
	fmt.Printf("  -p, --passphrase    Private key decryption password.\n")
	fmt.Printf("  -h, --hostkey-file  Specify the known-hosts file. Defaulted. Disable using: -H \"\"\n")
	fmt.Printf("  -v, --verbose       Verbose mode.  Prints progress debug messages.\n")
	fmt.Printf("  -vv, --very-verbose Verbose mode.  Prints progress debug messages.\n")
	fmt.Printf("  -V, --version       Display version information.\n")
	fmt.Printf("  -t, --tunnel        Define a tunnel:\n")
	fmt.Printf("                        -t \"local:port:remote:port:forward:port\"\n")
	fmt.Printf("                        -t \"port:remote:port:forward:port\"  (local->127.0.0.1)\n")
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
