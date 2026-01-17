#!/bin/sh

PROGNAME=ptftp

# build targets
$(PROGNAME): *.go */*.go
	@env GOPATH=/tmp/go go mod vendor && env GOPATH=/tmp/go CGO_ENABLED=0 go build -trimpath -o $(PROGNAME)
	@-strip $(PROGNAME) 2>/dev/null || true
	@-#upx -9 $(PROGNAME) 2>/dev/null || true
update:
	@rm -f go.sum && echo "module $(PROGNAME)\n\ngo 1.25" >go.mod && env GOPATH=/tmp/go GOPROXY=direct go get
lint:
	@-go vet ./... || true
	@-staticcheck ./... || true
	@-gocritic check -enableAll ./... || true
	@-govulncheck ./... || true
distclean:
	@rm -rf $(PROGNAME) *.upx vendor

# run targets
version: $(PROGNAME)
	@./$(PROGNAME) version
client: $(PROGNAME)
	@./$(PROGNAME) localhost:6969 alpine.iso
server: ptftp
	@./$(PROGNAME) server ptftp.conf
