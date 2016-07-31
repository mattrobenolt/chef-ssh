FROM golang:1.6

RUN mkdir -p /go/src/app
WORKDIR /go/src/app

ENV CROSSPLATFORMS \
        linux/amd64 linux/386 linux/arm \
        darwin/amd64 darwin/386 \
        freebsd/amd64 freebsd/386 freebsd/arm

ENV GOARM 5

CMD set -x \
    && go-wrapper download \
    && for platform in $CROSSPLATFORMS; do \
            GOOS=${platform%/*} \
            GOARCH=${platform##*/} \
                go build -v -o bin/chef-ssh-${platform%/*}-${platform##*/}; \
    done
