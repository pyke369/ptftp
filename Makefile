#!/bin/sh

# default target
ptftp: *.go
	@export CGO_ENABLED=0; go build -o ptftp *.go && strip ptftp

# build targets
deb:
	@debuild -e GOROOT -e PATH -i -us -uc -b
clean:
distclean: clean
	@rm -rf ptftp
debclean:
	@debuild clean
	@rm -f ../ptftp_*

# run targets
client: ptftp
	@./ptftp localhost pxelinux.0
server: ptftp
	@ulimit -n 65536 && ./ptftp server ptftp.conf
