package main

import (
	"flag"
	"fmt"
	"github.com/MeetFrankie/txn-id-server/txnid"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
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
	PROGRESS_FILESIZE int    = 32
	MAX_START         int    = 32767
	MAX_INCREMENT     int    = 32767
)

type config struct {
	verbose      bool
	daemon       bool
	ip           string
	port         int
	base         uint64
	start        int // needs to be typecast to uint64 later
	increment    int // needs to be typecast to uint16 later
	confdir      string
	progressfile string
	usingfile    bool
}

// _stop: Global Stop semaphore
var (
	_stop chan bool = make(chan bool, 50) // overkill probably, but we do want to get the signal.
)

// Boring but useful debug printer.
func __debug(s string) {
	fmt.Println("DEBUG: ", s)
}

func InitUsage() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n%s  [-verbose] [-daemon] [-ip n.n.n.n] [-port n] [-instance n] [-increment n] [-confdir /path/to/files]\n\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Runs a transaction ID service, listening on a port for a connection, then writes a number and closes the socket.\n\n")
		flag.PrintDefaults()
		os.Exit(-1)
	}
}

func _Exit(err string, usage bool) {
	fmt.Fprintf(flag.CommandLine.Output(), "%s error: %s\n\n", os.Args[0], err)
	if usage {
		flag.Usage()
	}
}

// InitEnv pulls in environment vars to override the defaults.
// No serious validity checking is done, we'll do that after we further
// override with commandline flags.
func InitEnv(cfg *config) *config {

	cfg.verbose = DEFAULT_VERBOSE
	verbose_str := os.Getenv("TXNID_SERVER_VERBOSE")
	if verbose_str == "1" || verbose_str == "true" || verbose_str == "TRUE" || verbose_str == "True" {
		cfg.verbose = true
	}

	cfg.daemon = DEFAULT_DAEMON
	daemon_str := os.Getenv("TXNID_SERVER_DAEMONISE")
	if daemon_str == "1" || daemon_str == "true" || daemon_str == "TRUE" || daemon_str == "True" {
		cfg.daemon = true
	}

	cfg.ip = DEFAULT_IP
	rawIP_str := os.Getenv("TXNID_SERVER_LISTENIP")
	if rawIP_str != "" {
		cfg.ip = rawIP_str
	}

	cfg.port = DEFAULT_PORT
	rawPort_str := os.Getenv("TXNID_SERVER_LISTENPORT")
	if rawPort_str != "" {
		if p, err := strconv.Atoi(rawPort_str); err == nil && p > 1 && p < 65535 {
			cfg.port = p
		} else {
			_Exit(fmt.Sprintf("Environment variable TXNID_SERVER_LISTENPORT (%s) is not a valid port number", rawPort_str), true)
		}
	}

	cfg.start = DEFAULT_START
	rawStart_str := os.Getenv("TXNID_SERVER_INSTANCE")
	if rawStart_str != "" {
		if p, err := strconv.Atoi(rawStart_str); err == nil && p >= 1 && p <= MAX_START {
			cfg.start = p
		} else {
			_Exit(fmt.Sprintf("Environment variable TXNID_SERVER_INSTANCE (%s) is not a valid instance number", rawStart_str), true)
		}
	}

	cfg.increment = DEFAULT_INCREMENT
	rawIncrement_str := os.Getenv("TXNID_SERVER_INCREMENT")
	if rawIncrement_str != "" {
		if p, err := strconv.Atoi(rawIncrement_str); err == nil && p >= 1 && p <= MAX_INCREMENT {
			cfg.increment = p
		} else {
			_Exit(fmt.Sprintf("Environment variable TXNID_SERVER_INCREMENT (%s) is not a valid increment", rawIncrement_str), true)
		}
	}

	cfg.confdir = DEFAULT_CONFDIR
	rawConfDir_str := os.Getenv("TXNID_SERVER_CONFDIR")
	if rawConfDir_str != "" {
		cfg.confdir = rawConfDir_str
	}

	return cfg
}

func InitFlags(cfg *config) *config {

	flag.BoolVar(&(cfg.verbose), "verbose", cfg.verbose, "If set to true, then be verbose, printing start params and progress file rollover")
	flag.BoolVar(&(cfg.daemon), "daemon", cfg.daemon, "If set to true, then spawn a child process and run in the background")
	flag.StringVar(&(cfg.ip), "ip", cfg.ip, "If set, only listen on the specified IP address")
	flag.IntVar(&(cfg.port), "port", cfg.port, "If set, list on port N")
	flag.IntVar(&(cfg.start), "instance", cfg.start, "If running a cluster of txn id servers, what is the instance number? (Also the starting number)")
	flag.IntVar(&(cfg.increment), "increment", cfg.increment, "If set, how many does each successive number increment by. If in a cluster, this must be the total instances running in said cluster")
	flag.StringVar(&(cfg.confdir), "confdir", cfg.confdir, "If set, defines the location of the progress file")

	flag.Parse()

	return cfg
}

func ValidateStartParams(cfg *config) *config {

	// These won't be changing
	// cfg.verbose  // hard to screw up
	// cfg.daemon   // same
	// cfg.ip       // will either work or not when we try to open it. Let the OS sort it out.

	if cfg.port <= 1 || cfg.port >= 65535 {
		_Exit(fmt.Sprintf("Commandline -port value [%d] is not a valid port number", cfg.port), true)
	}

	if cfg.start <= 0 || cfg.start >= MAX_START {
		_Exit(fmt.Sprintf("Commandline -instance value [%d] is not a valid number", cfg.start), true)
	}

	if cfg.increment <= 1 || cfg.increment >= MAX_INCREMENT {
		_Exit(fmt.Sprintf("Commandline -increment value [%d] is not a valid number", cfg.increment), true)
	}

	if cfg.start > cfg.increment {
		_Exit(fmt.Sprintf("Commandline -instance [%d] must be less than -increment [%d]", cfg.start, cfg.increment), true)
	}

	cfg.progressfile = cfg.confdir + "/" + PROGRESS_FILENAME
	if err := LoadProgressFile(cfg); err != nil {
		_Exit(fmt.Sprintf("Cannot parse progress file [%s]: %s", cfg.progressfile, err.Error()), true)
	}

	return cfg
}

func LoadProgressFile(cfg *config) error {

	// max bytes possible
	var (
		pf   *os.File
		ferr error
	)
	fileb := make([]byte, PROGRESS_FILESIZE)

	if pf, ferr = os.OpenFile(cfg.progressfile, os.O_RDWR|os.O_CREATE, 0600); ferr != nil {
		_Exit(fmt.Sprintf("Cannot open progress file [%s]: %s", cfg.progressfile, ferr.Error()), true)
	}
	defer pf.Close()

	num, err := pf.Read(fileb)

	if num > 0 && num <= 32 && (err == nil || err == io.EOF) {
		var b, c, i uint64

		s := strings.Split(string(fileb[:num]), "|")
		if len(s) >= 3 {
			if b, err = strconv.ParseUint(s[0], 10, 64); err != nil {
				return fmt.Errorf("Cannot parse base field [%s]: [%s]", s[0], err.Error())
			}

			if !strings.HasPrefix(s[1], "~") {
				if c, err = strconv.ParseUint(s[1], 10, 16); err != nil {
					return fmt.Errorf("Cannot parse counter field [%s]: [%s]", s[1], err.Error())
				}
			} else {
				c = uint64(cfg.start)
			}

			if !strings.HasPrefix(s[2], "~") {
				if i, err = strconv.ParseUint(s[2], 10, 16); err != nil {
					return fmt.Errorf("Cannot parse increment field [%s]: [%s]", s[2], err.Error())
				}
			}

			// All good I think!
			cfg.base = b
			cfg.start = int(c)
			if i > 0 {
				cfg.increment = int(i)
			}
			if cfg.verbose {
				fmt.Printf("Loaded starting values from progress file [%s]: base [%d], start [%d], increment [%d]\n", cfg.progressfile, cfg.base, cfg.start, cfg.increment)
			}

			// Yes, we are.
			cfg.usingfile = true

		} else {
			return fmt.Errorf("Could not parse progress file data [%s]", string(fileb))
		}
	} else {
		return fmt.Errorf("Error reading progress file data: [%s] [%s]", string(fileb), err.Error())
	}

	return nil
}

func WritePartialProgressFile(b uint64, progFile string) error {
	return WriteFullProgressFile(&b, nil, nil, progFile)
}

func WriteFullProgressFile(b, c *uint64, i *uint16, progFile string) error {

	var (
		pf  *os.File
		err error
	)
	s := make([]byte, PROGRESS_FILESIZE, PROGRESS_FILESIZE)

	if pf, err = os.OpenFile(progFile, os.O_RDWR|os.O_CREATE, 0600); err != nil {
		// Shouldn't happen as we checked previously, but you never know.
		return err
	}
	defer pf.Close()

	if c != nil && i != nil {
		s = []byte(fmt.Sprintf("%d|%d|%d|", *b, *c, *i))
	} else {
		s = []byte(fmt.Sprintf("%d|~|~|", *b))
	}

	if _, err = pf.Write(s); err != nil {
		return err
	}

	return nil

}

func snapAndStop(sas chan os.Signal, n *txnid.Numbers, cfg *config) {
	sig := <-sas // wait for a signal
	b, c, i := n.Snapshot(true)

	if cfg.verbose {
		fmt.Printf("Signal [%v] caught: snapshot b[%d], c[%d], i[%d]\n", sig, b, c, i)
	}

	if err := WriteFullProgressFile(&b, &c, &i, cfg.progressfile); err != nil {
		fmt.Fprintf(os.Stderr, "Critical!! Error writing file [%s]: %s", cfg.progressfile, err.Error())
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
		fmt.Printf("Listen error on port [%s:%d] - %s", ip, port, err.Error())
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

	// Pre-prep our Usage function to override the flag packages default.
	InitUsage()

	// First we extract the ENV vars
	// Then we override with commandline params
	// Then we'll load in a save file.
	// Lastly we'll validate the basics and off we go.
	cfg := &config{}
	cfg = ValidateStartParams(InitFlags(InitEnv(cfg)))

	// OK, now we have all our parameters, let's get things set up.
	numGen := txnid.InitNumbers(cfg.base, uint64(cfg.start), uint16(cfg.increment))

	// Set this callback to safe store the base counter.
	// MUST ensure this is non-blocking and fast as it's inside a
	//    mutex locked function call.
	txnid.RollOverCallBack = func(b uint64) {
		if cfg.verbose {
			fmt.Printf("Rollover to base of: %d. Writing %d to %s\n", b, b+1, cfg.progressfile)
		}
		// Write b+1 to safe store in case of crash/hard stop.
		if err := WritePartialProgressFile(b+1, cfg.progressfile); err != nil {
			fmt.Fprintf(os.Stderr, "Critical!! Error writing progress file [%s]: %s", cfg.progressfile, err.Error())
		}
	}

	// OK, so what are the params we settled on? Let's print them.
	if cfg.verbose {
		fmt.Println("Transaction ID Server\n------------------------------------------")
		fmt.Println("Parameters:")
		fmt.Println("\tVerbose:              on (obviously)")
		fmt.Println("\tDaemon Mode (N/A):   ", cfg.daemon)
		fmt.Println("\tListener IP:         ", cfg.ip)
		fmt.Println("\tListener Port:       ", cfg.port)
		fmt.Println("\tInstance:            ", cfg.start)
		fmt.Println("\tIncrement:           ", cfg.increment)
		fmt.Println("\tProgress File:       ", cfg.progressfile)
		fmt.Println("\tProgress File Loaded:", cfg.usingfile)
		fmt.Println("------------------------------------------\nStart:  ", numGen)
	}

	// Ignore HUP signals - we don't want to reload.
	signal.Ignore(syscall.SIGHUP)

	// Set up signal capture for INT and TERM to safely shutdown
	// after saving to disk. Spawn a listener go routine to await.
	signalCapture := make(chan os.Signal, 1)
	signal.Notify(signalCapture, syscall.SIGINT, syscall.SIGTERM)
	go snapAndStop(signalCapture, numGen, cfg)

	// Right, all set? Let's go!
	// Should continue until the signal semaphore is received via
	// snapAndStop()
	Listen(numGen, cfg.ip, cfg.port)

	// Wait a few seconds for everything to tidy up.
	// Probably not necessary.
	time.Sleep(3 * time.Second)

	// Final output if being verbose.
	if cfg.verbose {
		fmt.Println("Finish: ", numGen)
	}
}
