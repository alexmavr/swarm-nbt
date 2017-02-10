FROM golang:1.7.1-alpine

ARG GOOS
ARG GOARCH

COPY . /go/src/github.com/alexmavr/swarm-benchnet
WORKDIR /go/src/github.com/alexmavr/swarm-benchnet

RUN set -ex && apk add --no-cache --virtual .build-deps git && go get github.com/tools/godep 
RUN GOARCH=$GOARCH GOOS=$GOOS CGO_ENABLED=0 godep go install -v -a -tags netgo -installsuffix netgo -ldflags "-w -X github.com/alexmavr/swarm-benchnet/version.GITCOMMIT=$(git rev-parse --short HEAD) -X github.com/alexmavr/swarm-benchnet/version.BUILDTIME=$(date -u +%FT%T%z)" \
	&& apk del .build-deps

EXPOSE 4443
ENTRYPOINT ["/go/bin/swarm-benchnet"]
