package main

import (
	"flag"
	"fmt"
	"github.com/MeetFrankie/txn-id-server/txnid"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const (
	DEFAULT_VERBOSE   bool   = false
	DEFAULT_DAEMON    bool   = false
	DEFAULT_IP        string = ""
	DEFAULT_PORT      int    = 8963
	DEFAULT_START     int    = 0
	DEFAULT_INCREMENT int    = 1
	DEFAULT_CONFDIR   string = "/etc/txnidserver/"
	PROGRESS_FILENAME string = "txnid.dat"
	MAX_START         int    = 32767
	MAX_INCREMENT     int    = 32767
)

// _stop: Global Stop semaphore
// _progressFile: confirmed location of the progress file
var (
	_stop         chan bool = make(chan bool, 50) // overkill probably, but we do want to get the signal.
	_progressFile string
)

func InitUsage() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n%s  [-verbose] [-daemon] [-ip n.n.n.n] [-port n] [-instance n] [-increment n] [-confdir /path/to/files]\n\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Runs a transaction ID service, listening on a port for a connection, then writes a number and closes the socket.\n\n")
		flag.PrintDefaults()
		os.Exit(-1)
	}
}

func _Exit(err string) {
	fmt.Fprintf(flag.CommandLine.Output(), "%s error: %s\n\n", os.Args[0], err)
	flag.Usage()
}

// InitEnv pulls in environment vars to override the defaults.
// No serious validity checking is done, we'll do that after we further
// override with commandline flags.
func InitEnv() (verbose, daemon bool, rawIP string, rawPort int, rawStart int, rawIncrement int, rawConfDir string) {

	verbose = DEFAULT_VERBOSE
	verbose_str := os.Getenv("TXNID_SERVER_VERBOSE")
	if verbose_str == "1" || verbose_str == "true" || verbose_str == "TRUE" || verbose_str == "True" {
		verbose = true
	}

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
			_Exit(fmt.Sprintf("Environment variable TXNID_SERVER_LISTENPORT (%s) is not a valid port number", rawPort_str))
		}
	}

	rawStart = DEFAULT_START
	rawStart_str := os.Getenv("TXNID_SERVER_INSTANCE")
	if rawStart_str != "" {
		if p, err := strconv.Atoi(rawStart_str); err == nil && p >= 1 && p <= MAX_START {
			rawStart = p
		} else {
			_Exit(fmt.Sprintf("Environment variable TXNID_SERVER_INSTANCE (%s) is not a valid instance number", rawStart_str))
		}
	}

	rawIncrement = DEFAULT_INCREMENT
	rawIncrement_str := os.Getenv("TXNID_SERVER_INCREMENT")
	if rawIncrement_str != "" {
		if p, err := strconv.Atoi(rawIncrement_str); err == nil && p >= 1 && p <= MAX_INCREMENT {
			rawIncrement = p
		} else {
			_Exit(fmt.Sprintf("Environment variable TXNID_SERVER_INCREMENT (%s) is not a valid increment", rawIncrement_str))
		}
	}

	rawConfDir = DEFAULT_CONFDIR
	rawConfDir_str := os.Getenv("TXNID_SERVER_CONFDIR")
	if rawConfDir_str != "" {
		rawConfDir = rawConfDir_str
	}

	return
}

func InitFlags(default_verbose, default_daemon bool, default_IP string, default_Port int, default_Start int, default_Increment int, default_ConfDir string) (verbose, daemon bool, rawIP string, rawPort int, rawStart int, rawIncrement int, rawConfDir string) {

	flag.BoolVar(&verbose, "verbose", default_verbose, "If set to true, then be verbose, printing start params and progress file rollover")
	flag.BoolVar(&daemon, "daemon", default_daemon, "If set to true, then spawn a child process and run in the background")
	flag.StringVar(&rawIP, "ip", default_IP, "If set, only listen on the specified IP address")
	flag.IntVar(&rawPort, "port", default_Port, "If set, list on port N")
	flag.IntVar(&rawStart, "instance", default_Start, "If running a cluster of txn id servers, what is the instance number? (Also the starting number)")
	flag.IntVar(&rawIncrement, "increment", default_Increment, "If set, how many does each successive number increment by. If in a cluster, this must be the total instances running in said cluster")
	flag.StringVar(&rawConfDir, "confdir", default_ConfDir, "If set, defines the location of the progress file")

	flag.Parse()

	return
}

func ValidateStartParams(raw_verbose, raw_daemon bool, raw_IP string, raw_Port int, raw_Start int, raw_Increment int, raw_ConfDir string) (verbose, daemon bool, IP string, Port int, Start int, Increment int, progressFile string) {

	// These won't be changing
	verbose = raw_verbose
	daemon = raw_daemon

	IP = raw_IP

	if raw_Port >= 1 && raw_Port <= 65535 {
		Port = raw_Port
	} else {
		_Exit(fmt.Sprintf("Commandline -port value [%d] is not a valid port number", raw_Port))
	}

	if raw_Start >= 0 && raw_Start <= MAX_START {
		Start = raw_Start
	} else {
		_Exit(fmt.Sprintf("Commandline -instance value [%d] is not a valid number", raw_Start))
	}

	if raw_Increment >= 1 && raw_Increment <= MAX_INCREMENT {
		Increment = raw_Increment
	} else {
		_Exit(fmt.Sprintf("Commandline -increment value [%d] is not a valid number", raw_Increment))
	}

	if raw_Start > raw_Increment {
		_Exit(fmt.Sprintf("Commandline -instance [%d] must be less than -increment [%d]", raw_Start, raw_Increment))
	}

	progressFile = raw_ConfDir + "/" + PROGRESS_FILENAME

	return
}

func WritePartialProgressFile(b uint64, progFile string) {
	WriteFullProgressFile(&b, nil, nil, progFile)
}

func WriteFullProgressFile(b, c *uint64, i *uint16, progFile string) error {

	var (
		pf  *os.File
		err error
		s   string
	)

	if pf, err = os.OpenFile(progFile, os.O_RDWR|os.O_CREATE, 700); err != nil {
		return err
	}
	defer pf.Close()

	if c != nil && i != nil {
		s = fmt.Sprintf("%d|%d|%d", *b, *c, *i)
	} else {
		s = fmt.Sprintf("%d||", *b)
	}

	if _, err = pf.WriteString(s); err != nil {
		return err
	}

	return nil

}

func snapAndStop(sas chan os.Signal, n *txnid.Numbers, progFile string) {
	_ = <-sas // wait for a signal
	b, c, i := n.Snapshot(true)

	fmt.Printf("Signal caught: snapshot b[%d], c[%d], i[%d]\n", b, c, i)

	if err := WriteFullProgressFile(&b, &c, &i, progFile); err != nil {
		fmt.Printf("Error writing file: %s", err.Error())
	}

	return
}

func handleConnection(conn net.Conn, n *txnid.Numbers) {
	defer conn.Close()

	var s string

	i, stopped := n.GetNextNum()
	if !stopped {
		s = fmt.Sprintf("%d\n", i)
	} else {
		s = "-1\n"
	}
	conn.Write([]byte(s))

	_stop <- stopped
}

func Listen(n *txnid.Numbers, ip string, port int) {

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ip, port))

	if err != nil {
		fmt.Println("Listen error: ", err.Error())
		return
	}

	stopnow := false
	for stopnow == false {
		conn, err := ln.Accept()
		if err != nil {
			// handle error
			fmt.Println("Accept error: ", err.Error())
			break
		}
		go handleConnection(conn, n)

		stopnow = <-_stop
	}
}

func main() {

	InitUsage()

	verbose, daemon, IP, Port, Start, Increment, _progressFile := ValidateStartParams(InitFlags(InitEnv()))

	numGen := txnid.InitNumbers(0, uint64(Start), uint16(Increment))
	txnid.RollOverCallBack = func(b uint64) {
		if verbose {
			fmt.Printf("Rollover to base of: %d. Writing %d to %s\n", b, b+1, _progressFile)
			WritePartialProgressFile(b+1, _progressFile)
		}
	}

	if verbose {
		fmt.Println("Transaction ID Server\n------------------------------------------")
		fmt.Println("Parameters:")
		fmt.Println("\tVerbose:       on (obviously)")
		fmt.Println("\tDaemon Mode:  ", daemon)
		fmt.Println("\tListener IP:  ", IP)
		fmt.Println("\tListener Port:", Port)
		fmt.Println("\tInstance:     ", Start)
		fmt.Println("\tIncrement:    ", Increment)
		fmt.Println("\tProgress File:", _progressFile)
		fmt.Println("------------------------------------------\nStart:  ", numGen)
	}

	signalCapture := make(chan os.Signal, 1)
	signal.Notify(signalCapture, syscall.SIGHUP)
	go snapAndStop(signalCapture, numGen, _progressFile)

	Listen(numGen, IP, Port)

	time.Sleep(5 * time.Second)

	if verbose {
		fmt.Println("Finish: ", numGen)
	}
}
