package main

import (
	"errors"
	"net"
	"os"
	"strings"
	"sync"
)

type AddrList struct {
	List  []*net.TCPAddr
	Mutex sync.RWMutex
}

func (al *AddrList) Add(addr ...*net.TCPAddr) {
	defer al.Mutex.Unlock()
	al.Mutex.Lock()
	al.List = append(al.List, addr...)
}

func (al *AddrList) Contains(addr *net.TCPAddr) bool {
	addrStr := addr.IP.String()
	defer al.Mutex.RUnlock()
	al.Mutex.RLock()
	for _, k := range al.List {
		if addrStr == k.IP.String() {
			return true
		}
	}
	return false
}

//Remove removes an address from the list based solely on its IP (not source port)
func (al *AddrList) Remove(addr *net.TCPAddr) bool {
	defer al.Mutex.Unlock()

	addrStr := addr.IP.String()
	al.Mutex.Lock()
	llen := len(al.List)
	for i, k := range al.List {
		if k.IP.String() != addrStr {
			continue
		}
		al.List[i] = al.List[llen-1]
		al.List = al.List[:llen-1]
		return true
	}
	return false
}

//RemoveStrict removes an address from the list based on its IP AND source port
//TODO: because TCPAddr's are shared (as pointers) between lists, updating the port
//for one likely updates the port on any other list.  This means that the p2p program
//will not gracefully hadle multiple peers connecting from behind the same router disconnecting, etc
func (al *AddrList) RemoveStrict(addr *net.TCPAddr) bool {
	defer al.Mutex.Unlock()

	addrStr := addr.IP.String()
	al.Mutex.Lock()
	llen := len(al.List)
	for i, k := range al.List {
		if k.IP.String() != addrStr {
			continue
		}
		if k.Port != addr.Port {
			continue
		}
		al.List[i] = al.List[llen-1]
		al.List = al.List[:llen-1]
		return true
	}
	return false
}

func (al *AddrList) Len() int {
	defer al.Mutex.RUnlock()
	al.Mutex.RLock()
	return len(al.List)
}

func (al *AddrList) Do(doFunc func(lst []*net.TCPAddr)) {
	defer al.Mutex.RUnlock()
	al.Mutex.RLock()
	doFunc(al.List)
}

func (al *AddrList) UpdatePort(addr *net.TCPAddr, newPort int) bool {
	defer al.Mutex.Unlock()
	al.Mutex.Lock()

	ipStr := addr.IP.String()
	for _, k := range al.List {
		if k.IP.String() != ipStr {
			continue
		}
		k.Port = newPort
		return true
	}
	return false
}

//Difference takes an input AddrList and returns the difference, that is
//it only returns items that are in the original list and not the other list
//the result slice is passed by reference because this function is called in a
//fast running infinite loop, so that if a new slice was allocated each time
//it could significantly impact the memory usage of the program
func (al *AddrList) Difference(other *AddrList, result *[]*net.TCPAddr) {
	defer func() {
		al.Mutex.RUnlock()
		other.Mutex.RUnlock()
	}()
	al.Mutex.RLock()
	other.Mutex.RLock()

	otherIPs := make(map[string]bool)
	for _, k := range other.List {
		otherIPs[k.IP.String()] = true
	}

	for _, k := range al.List {
		ipstr := k.IP.String()
		if _, ok := otherIPs[ipstr]; ok == false {
			*result = append(*result, k)
		}
	}
}

func GetLocalIP() (string, error) {
	/*
		ifaces, err := net.Interfaces()
		if err != nil {
			return "", err
		}

		for _,iface := range ifaces {

			addrs, err := iface.Addrs()
			if err != nil { continue; }
			if iface.Flags & net.FlagLoopback != 0 { continue; }
			if iface.Flags & net.FlagUp == 0 { continue; }

			for _,addr := range addrs {
				log.Printf("ADDRESS: %s\n", addr.String())
			}
		}
		return "???",nil
	*/
	name, err := os.Hostname()
	if err != nil {
		return "", err
	}

	addrs, err := net.LookupHost(name)
	if err != nil {
		return "", err
	}

	for _, a := range addrs {
		addr := strings.Split(a, "/")[0]
		octects := strings.Split(addr, ".")
		if len(octects) == 4 {
			return addr, nil
		}
	}
	return "", errors.New("no ipv4 interface available")
}
