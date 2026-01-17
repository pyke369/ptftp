package main

import (
	"os"
	"path/filepath"
	"strings"

	c "ptftp/client"
	"ptftp/common"
	s "ptftp/server"
)

func usage() {
	progname := filepath.Base(os.Args[0])
	os.Stderr.WriteString("usage:\n\n" +
		progname + " version\n" +
		progname + " server <configuration>\n" +
		progname + " <host>[:<port>] <remote> [<local>]\n",
	)
	os.Exit(1)
}

func main() {
	if len(os.Args) > 1 {
		switch strings.ToLower(os.Args[1]) {
		case "version":
			os.Stdout.WriteString(common.PROGNAME + " v" + common.PROGVER + "\n")

		case "server":
			s.Run()

		default:
			if len(os.Args) < 3 {
				usage()
			}
			c.Run()
		}

	} else {
		usage()
	}
}
