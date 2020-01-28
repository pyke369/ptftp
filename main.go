package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	progname = "ptftp"
	version  = "1.2.1"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n"+
		"  %s version\n"+
		"  %s server [<configuration file>]\n"+
		"  %s <host[:<port>]> <remote filename> [<local filename>]\n",
		progname, progname, progname)
	os.Exit(1)
}

func main() {
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "version":
			fmt.Printf("%s v%s\n", progname, version)
		case "server":
			server()
		default:
			if len(os.Args) < 3 {
				usage()
			}
			client()
		}
	} else {
		usage()
	}
}
