package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	progname = "ptftp"
	version  = "1.0.1"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n"+
		"  %s version\n"+
		"  %s server [<configuration file>]\n"+
		"  %s <host[:<port>]> <remote name> <local file>\n",
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
			usage()
		}
	} else {
		usage()
	}
}
