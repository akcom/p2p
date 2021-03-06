package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type conn struct {
	remoteAddr *net.TCPAddr
	server     *Node
	rwc        *net.TCPConn

	buf *bufio.ReadWriter
}

//Node is the basic structure of the P2P network
type Node struct {
	listener          *net.TCPListener
	LocalAddr         *net.TCPAddr
	ActiveConnections int
	MaxConnections    int

	KnownAddrs     AddrList
	ConnectedAddrs AddrList
	StaleAddrs     AddrList
	//mutex is used for any of the lists
	ListMutex sync.RWMutex
}

var (
	//rwPool is used to provide a pool of ReadWriters() to prevent wasted resources
	rwPool sync.Pool
)

//NewNode returns an initialized node structure ready to listen/connect.
//peers can be nil, in which case the KnownPeers is empty but initialized
func NewNode(listenPort, MaxConnections int) (res *Node, err error) {
	//TODO: support IPv6 in the future
	strPort := strconv.Itoa(listenPort)
	strLocal, err := GetLocalIP()
	if err != nil {
		return
	}
	localAddrString := strLocal + ":" + strPort
	LocalAddr, err := net.ResolveTCPAddr("tcp", localAddrString)
	if err != nil {
		return
	}
	res = new(Node)
	res.LocalAddr = LocalAddr
	res.MaxConnections = MaxConnections
	return
}

//Listen starts a goroutine Serve() which accepts incoming connections
func (n *Node) Listen() error {
	var err error

	if n.listener, err = net.ListenTCP("tcp", n.LocalAddr); err != nil {
		return err
	}
	err = n.Serve()
	return err
}

//Serve accepts incoming connections
func (n *Node) Serve() error {
	defer n.listener.Close()

	log.Printf("listening for incoming connections on %v\n", n.listener.Addr().(*net.TCPAddr))

	tempDelay := 2 * time.Millisecond
	for {
		rwc, e := n.listener.AcceptTCP()
		if e != nil {
			//if there is an error and it is temporay, wait a certain amount of time before retrying
			//if the retry fails, double the wait time & repeat.  Max wait time is 1 second
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				tempDelay *= 2
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.Printf("accept error: %v; retrying in %v", ne, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 2 * time.Millisecond
		//initialize our new connection
		con, err := n.newConn(rwc)
		if err != nil {
			log.Printf("error creating connection: %v\n", err)
			continue
		}
		//add the new connection to the appropriate lists
		n.KnownAddrs.Add(con.remoteAddr)
		n.ConnectedAddrs.Add(con.remoteAddr)
		//remove it from stale in case it was in there from before
		n.StaleAddrs.Remove(con.remoteAddr)

		//serve the new connection
		go con.serve()
	}
}

func (n *Node) ConnectPeers() {
	//delay is used for sleeping when there are no valid peers to connect to
	delay := 1 * time.Second
	list := make([]*net.TCPAddr, 0, 100)
	for {
		//only connect to peers if we do not have enough
		if n.ConnectedAddrs.Len() >= n.MaxConnections {
			time.Sleep(delay)
			continue
		}
		//only connect if we know about some other peers and we're not already connected to them
		if (n.KnownAddrs.Len() == 0) || (n.KnownAddrs.Len() == n.ConnectedAddrs.Len()) {
			time.Sleep(delay)
			continue
		}
		//assemble a list of potential peers by taking KnownAddrs xor ConnectedAddrs
		n.KnownAddrs.Difference(&n.ConnectedAddrs, &list)
		if len(list) == 0 {
			time.Sleep(delay)
			continue
		}
		addr := list[rand.Intn(len(list))]
		log.Printf("attempting to connect to peer (%v)\n", addr)

		conn, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			//unable to connect, move it to the stale list
			log.Printf("unable to connect to peer (%v)\n", addr)
			n.StaleAddrs.Add(addr)
			n.KnownAddrs.Remove(addr)
			continue
		}
		n.ConnectedAddrs.Add(addr)
		c, err := n.newConn(conn)
		if err != nil {
			log.Fatalf("unable to create new connection context: %v", err)
		}
		c.writeln("PORT %v", c.server.LocalAddr.Port)
		go c.serve()
	}
}

//Start begins listening for incoming connections and also attempts to
//establish up to MaxConnection connections to known peers
//if no known peers are available, this node essentially becomes the basis for a new network
func (n *Node) Start() {
	go n.ConnectPeers()
	go n.Listen()
}

//newConn takes a TCPConn and returns a connection structure which
//contains all the relevant contextual information
func (n *Node) newConn(rwc *net.TCPConn) (*conn, error) {
	c := new(conn)
	c.remoteAddr = rwc.RemoteAddr().(*net.TCPAddr)
	c.server = n
	c.rwc = rwc

	var ok bool
	c.buf, ok = rwPool.Get().(*bufio.ReadWriter)
	if c.buf == nil || ok == false {
		c.buf = bufio.NewReadWriter(
			bufio.NewReader(c.rwc),
			bufio.NewWriter(c.rwc))
	} else {
		c.buf.Reader.Reset(c.rwc)
		c.buf.Writer.Reset(c.rwc)
	}

	return c, nil
}

//writeln just takes the input, formats it, adds a '\n', puts it in the write buffer and flushes the buffer
func (c *conn) writeln(format string, a ...interface{}) {
	c.buf.WriteString(fmt.Sprintf(format+"\n", a...))
	c.buf.Flush()
}

//serve processes the io for a connection
func (c *conn) serve() {
	var (
		cmd  string
		args []string
	)

	//check if we need to just send a peer list and disconnect
	if c.server.ConnectedAddrs.Len() > c.server.MaxConnections {
		//remove this peer from the connected list
		c.server.ConnectedAddrs.RemoveStrict(c.remoteAddr)
		//send a random peer list and gracefully close the connection
		//peers are pulled from the KnownAddr list
		c.server.KnownAddrs.Mutex.RLock()
		lst := c.server.KnownAddrs.List
		//send a maximum of 10 peers
		numPeers := 10
		if len(lst) < 11 {
			//we subtract 1, otherwise the peer will get itself in the list
			numPeers = len(lst) - 1
		}
		peerList := make([]*net.TCPAddr, numPeers)
		//randomize the KnownAddrs list
		perm := rand.Perm(len(lst))
		//offset is used to make sure the peer does not get its own address
		offset := 0
		ipStr := c.remoteAddr.String()
		for i := 0; i < numPeers; i++ {
			//make sure we do not send this peer its own address in the list
			if lst[perm[i+offset]].IP.String() == ipStr {
				offset++
			}
			peerList[i] = lst[perm[i+offset]]
		}
		c.server.KnownAddrs.Mutex.RUnlock()

		c.writeln("PEERS %v", peerList)
		c.rwc.Close()
		rwPool.Put(c.buf)
		return
	}

	for {
		line, err := c.buf.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Unable to read (%v)\n", err)
			}
			//shuffle the lists as necessary
			c.server.StaleAddrs.Add(c.remoteAddr)
			c.server.ConnectedAddrs.RemoveStrict(c.remoteAddr)
			c.server.KnownAddrs.RemoveStrict(c.remoteAddr)
			break
		}
		line = strings.TrimSpace(line)
		if len(line) < 1 {
			continue
		}
		split := strings.Split(line, " ")

		cmd = split[0]
		if len(split) > 1 {
			args = split[1:]
		} else {
			args = nil
		}
		switch {
		case cmd == "STALE":
			//handle this
			c.server.StaleAddrs.Do(func(list []*net.TCPAddr) {
				c.writeln("STALE %v", list)
			})
		case cmd == "CONN":
			c.server.ConnectedAddrs.Do(func(list []*net.TCPAddr) {
				c.writeln("CONN %v", list)
			})
		case cmd == "KNOWN":
			c.server.KnownAddrs.Do(func(list []*net.TCPAddr) {
				c.writeln("KNOWN %v", list)
			})
		case cmd == "PORT":
			//peer is telling us what port it listens on
			if len(args) < 1 {
				continue
			}
			port, err := strconv.Atoi(args[0])
			if err != nil {
				continue
			}
			c.server.KnownAddrs.UpdatePort(c.remoteAddr, port)
		default:
			log.Printf("Unknown command (%s)\n", cmd)
		}
		c.buf.Flush()
	}
}
