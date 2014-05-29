package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const DefaultPort = 9090
const DefaultMaxConnections = 16

//flags for parsing command line input
var (
	//BootstrapNode is the IP:Port of a bootstrapping node and is passed at the command line
	BootstrapNode string
	//ListenPort specifies the port that this node will listen for incoming connections on
	ListenPort int
	//MaxConnections specifies the maximum number of persistent peer connections
	//the number of connections may transiently exceed this # as connecting peers
	//are given a peer list and sent elsewhere
	MaxConnections int
	//PeerListFilename represents the path to a file containing a list of comma delimited in the form of IP:PORT
	PeerListFilename string
	//LogFile is the file where log data should be sent
	LogFilename string
)

//initFlags is responsible for intializing and parsing the command line flags structures
func initFlags() {
	flag.StringVar(&BootstrapNode, "bootstrap", "", "the botstrap peer node (IP:Port)")
	flag.IntVar(&ListenPort, "lport", DefaultPort, "the address to listen for incoming connections on")
	flag.StringVar(&PeerListFilename, "peers", "", "A filename containing a comma separated list of known peers")
	flag.IntVar(&MaxConnections, "maxcon", DefaultMaxConnections, "The maximum number of persistent peer connections")
	flag.StringVar(&LogFilename, "log", "", "The path of the file where log data should be sent")
	flag.Parse()
}

func AsyncResolve() (inChan chan string, outChan chan *net.TCPAddr, cancelChan chan bool) {
	inChan = make(chan string, 100)
	outChan = make(chan *net.TCPAddr, 100)
	cancelChan = make(chan bool, 1)
	go func() {
		//TODO: potential lock if no space available in outChan
		select {
		case <-cancelChan:
			return
		case strAddr := <-inChan:
			addr, err := net.ResolveTCPAddr("tcp", strAddr)
			if err != nil {
				outChan <- addr
			} else {
				outChan <- nil
			}
		}
	}()
	return
}

func main() {
	initFlags()
	if LogFilename != "" {
		f, err := os.OpenFile(LogFilename, os.O_RDWR|os.SEEK_END, 0666)
		if err != nil {
			log.Fatalf("Unable to open log file ('%s'): %s\n", LogFilename, err)
		}
		log.SetOutput(f)
		defer f.Close()
	}

	//setup our asynchronous resolution routine
	//inChan, outChan, cancelChan := AsyncResolve()
	//TODO: actually use asynchronous resolution

	//we must have at least a bootstrap node and/or a peer list
	if (BootstrapNode == "") && (PeerListFilename == "") {
		log.Printf("no bootstrap or peer list given, starting a new network")
	}

	peerList := make([]*net.TCPAddr, 0, 100)
	if BootstrapNode != "" {
		log.Printf("resolving bootstrap node address...\n")
		//name resolution can block for an excessive amount of time if a domain is provided instead of an IP, goroutine it
		addr, err := net.ResolveTCPAddr("tcp", BootstrapNode)
		if err != nil {
			log.Fatalf("Unable to resolve bootstrap node address ('%s'): %s\n", BootstrapNode, err)
		}
		peerList = append(peerList, addr)
	}

	//try to load a list
	if PeerListFilename != "" {
		log.Printf("loading peer file...\n")
		bytes, err := ioutil.ReadFile(PeerListFilename)
		if err != nil {
			log.Fatalf("Unable to read peer list file '%s': %s\n", PeerListFilename, err)
		}
		str := string(bytes)
		//parse the peer list asynchronously
		items := strings.Split(strings.Replace(str, "\n", "", -1), ",")
		for _, strAddr := range items {
			addr, err := net.ResolveTCPAddr("tcp", strAddr)
			if err != nil {
				log.Fatalf("unable to resolve peer ('%s'): %s\n", strAddr, err)
			}
			peerList = append(peerList, addr)
		}
	}

	log.Printf("initializing new node\n")
	node, err := NewNode(ListenPort, MaxConnections)
	if err != nil {
		log.Fatalf("Unable to initialize new node: %s\n", err)
	}
	node.KnownAddrs.Add(peerList...)
	node.Start()

	for {
		time.Sleep(1 * time.Second)
	}
}
