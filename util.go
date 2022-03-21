package main

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// This function does not make any actual connections
func GetOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("Could not get outbound IP")
	}
	return addr.IP, nil
}

func Cut(s, sep string) (before, after string) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):]
	}
	return s, ""
}

func Prefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	} else {
		return s
	}
}

type Set map[string]struct{}

func NewSet(a []string) Set {
	s := make(Set)
	for _, k := range a {
		s[k] = struct{}{}
	}
	return s
}

func (s Set) Array() []string {
	var a []string
	for k := range s {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}

func (s Set) Insert(k string) {
	s[k] = struct{}{}
}

func (s Set) Union(a Set) Set {
	u := make(Set)
	for k := range s {
		u[k] = struct{}{}
	}
	for k := range a {
		u[k] = struct{}{}
	}
	return u
}
