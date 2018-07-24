package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
)

type Numbers struct {
	lock      sync.Mutex
	base      uint64
	counter   uint64
	increment uint16
}

const (
	tickOver uint64 = 1 << 16
)

var (
	numgen Numbers
	once   sync.Once
)

func InitNumbers(b, c uint64, i uint16) *Numbers {
	once.Do(func() {
		numgen = Numbers{base: b << 16, counter: c, increment: i}
	})
	return &numgen
}

func (n *Numbers) GetNextNum() (z uint64) {
	n.lock.Lock()
	defer n.lock.Unlock()

	z = n.base | n.counter

	n.counter += uint64(n.increment)

	if n.counter >= tickOver {
		n.counter -= tickOver
		n.base += tickOver
	}

	return
}

func (n *Numbers) String() string {
	return fmt.Sprintf("--> Base: %d, Counter: %d, Increment: %d  -> Z %d", n.base, n.counter, n.increment, n.base|n.counter)
}

const (
	DEFAULT_DAEMON    bool   = false
	DEFAULT_IP        string = ""
	DEFAULT_PORT      int    = 8963
	DEFAULT_START     int    = 1
	DEFAULT_INCREMENT int    = 1
	DEFAULT_CONFDIR   string = "/etc/meetfrankie/txnid"
)

func Usage(err string) {
	fmt.Println(os.Args[0], ": Error:", err, "\n")

	fmt.Println("Runs a transaction ID service, listening on a port for a connection, then writes a number and closes the socket.")
	// TODO - a bit more info would be useful
	os.Exit(-1)
}

// InitEnv pulls in environment vars to override the defaults.
// No serious validity checking is done, we'll do that after we further
// override with commandline flags.
func InitEnv() (daemon bool, rawIP string, rawPort int, rawStart int, rawIncrement int, rawConfDir string) {

	daemon = DEFAULT_DAEMON
	daemon_str := os.Getenv("TXNID_SERVER_DAEMONISE")
	if daemon_str == "1" || daemon_str == "true" || daemon_str == "TRUE" || daemon_str == "True" {
		daemon = true
	}

	rawIP = DEFAULT_IP
	rawIP_str := os.Getenv("TXNID_SERVER_LISTENIP")
	if rawIP_str != "" {
		rawIP = rawIP_str
	}

	rawPort = DEFAULT_PORT
	rawPort_str := os.Getenv("TXNID_SERVER_LISTENPORT")
	if rawPort_str != "" {
		if p, err := strconv.Atoi(rawPort_str); err == nil && p > 1 && p < 65535 {
			rawPort = p
		} else {
			Usage(fmt.Sprintf("Environment variable TXNID_SERVER_LISTENPORT (%s) is not a valid port number", rawPort_str))
		}
	}

	rawStart = DEFAULT_START
	rawStart_str := os.Getenv("TXNID_SERVER_INSTANCE")
	if rawStart_str != "" {
		if p, err := strconv.Atoi(rawStart_str); err == nil && p >= 1 && p <= 65535 {
			rawStart = p
		} else {
			Usage(fmt.Sprintf("Environment variable TXNID_SERVER_INSTANCE (%s) is not a valid instance number", rawStart_str))
		}
	}

	rawIncrement = DEFAULT_INCREMENT
	rawIncrement_str := os.Getenv("TXNID_SERVER_INCREMENT")
	if rawIncrement_str != "" {
		if p, err := strconv.Atoi(rawIncrement_str); err == nil && p >= 1 && p <= 65535 {
			rawIncrement = p
		} else {
			Usage(fmt.Sprintf("Environment variable TXNID_SERVER_INCREMENT (%s) is not a valid increment", rawIncrement_str))
		}
	}

	rawConfDir = DEFAULT_CONFDIR
	rawConfDir_str := os.Getenv("TXNID_SERVER_CONFDIR")
	if rawConfDir_str != "" {
		rawConfDir = rawConfDir_str
	}

	return
}

func InitFlags(default_daemon bool, default_IP string, default_Port int, default_Start int, default_Increment int, default_ConfDir string) (daemon bool, rawIP string, rawPort int, rawStart int, rawIncrement int, rawConfDir string) {

	flag.BoolVar(&daemon, "daemon", default_daemon, "-daemon: if set to true, then spawn a child process and run in the background")
	flag.StringVar(&rawIP, "bindip", default_IP, "-bindip: if set, only listen on the specified IP address (default is all)")
	flag.IntVar(&rawPort, "port", default_Port, "-port: if set, list on port N (default is 8963)")
	flag.IntVar(&rawStart, "instance", default_Start, "-instance: if running a cluster of txn id servers, what is the instance number")
	flag.IntVar(&rawIncrement, "increment", default_Increment, "-increment: if set, how many does each successive number increment by. If in a cluster, this must be the total instances running in said cluster (default is 1)")
	flag.StringVar(&rawConfDir, "confdir", default_ConfDir, "-confdir: if set, only listen on the specified IP address (default is /etc/meetfrankie/txnid)")

	//	if err := flag.Parse(); err != nil {
	//		Usage(err.Error())
	//	}
	flag.Parse()

	return
}

func handleConnection(conn net.Conn, n *Numbers) {
	defer conn.Close()

	i := n.GetNextNum()
	s := fmt.Sprintf("%d\n", i)
	conn.Write([]byte(s))
	return
}

func Listen(n *Numbers, ip string, port int) {

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ip, port))

	if err != nil {
		// handle error
		fmt.Println("Listen error: ", err.Error())
		return
	}

	//	for i := 0; i < 10; i++ {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
			fmt.Println("Accept error: ", err.Error())
			break
		}
		go handleConnection(conn, n)
	}
}

func main() {

	daemon, rawIP, rawPort, rawStart, rawIncrement, rawConfDir := InitFlags(InitEnv())

	numGen := InitNumbers(0, 0, 1)

	fmt.Println("Start:  ", numGen)

	Listen(numGen, "10.2.20.119", 3333)

	//	var i uint64
	//
	//	for i < 65540 {
	//		i = foo.GetNextNum()
	//		fmt.Println(foo)
	//	}
	fmt.Println("Finish: ", numGen)

}
