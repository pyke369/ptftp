package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	PROGNAME = "ptftp"
	VERSION  = "1.2.3"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n"+
		"  %s version\n"+
		"  %s server [<configuration file>]\n"+
		"  %s <host[:<port>]> <remote filename> [<local filename>]\n",
		PROGNAME, PROGNAME, PROGNAME)
	os.Exit(1)
}

func main() {
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "version":
			fmt.Printf("%s v%s\n", PROGNAME, VERSION)
		case "server":
			Server()
		default:
			if len(os.Args) < 3 {
				usage()
			}
			Client()
		}
	} else {
		usage()
	}
}
