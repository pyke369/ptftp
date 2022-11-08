#!/bin/sh

# build targets
ptftp: *.go
	@env GOPATH=/tmp/go go get -d && env GOPATH=/tmp/go CGO_ENABLED=0 go build -trimpath -o ptftp
	@-strip ptftp 2>/dev/null || true
	@-upx -9 ptftp 2>/dev/null || true
clean:
distclean:
	@rm -rf ptftp *.upx
deb:
	@debuild -e GOROOT -e GOPATH -e PATH -i -us -uc -b
debclean:
	@debuild -- clean
	@rm -f ../ptftp_*

# run targets
client: ptftp
	@./ptftp localhost pxelinux.0
server: ptftp
	@ulimit -n 65536 && ./ptftp server ptftp.conf
