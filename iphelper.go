package main
import (
	"net"
	"os"
	"errors"
	"strings"
)

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
	
	for _,a := range addrs { 
		addr := strings.Split(a, "/")[0]
		octects := strings.Split(addr, ".")
		if len(octects) == 4 {
			return addr,nil
		}
	}
	return "", errors.New("no ipv4 interface available")
}