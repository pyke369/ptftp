#!/bin/sh

# build targets
ptftp: *.go
	@export GOPATH=/tmp/go; export CGO_ENABLED=0; go build -trimpath -o ptftp *.go && strip ptftp

clean:
distclean:
	@rm -rf ptftp
deb:
	@debuild -e GOROOT -e GOPATH -e PATH -i -us -uc -b
debclean:
	@debuild clean
	@rm -f ../ptftp_*

# run targets
client: ptftp
	@./ptftp localhost pxelinux.0
server: ptftp
	@ulimit -n 65536 && ./ptftp server ptftp.conf
