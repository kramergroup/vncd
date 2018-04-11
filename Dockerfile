FROM golang:latest AS builder
WORKDIR /go/src/github.com/kramergroup/vncd
RUN go get -d -v github.com/docker/docker/api \
                 github.com/docker/docker/client \
                 github.com/docker/go-connections/nat && \
    rm -rf /go/src/github.com/docker/docker/vendor/github.com/docker/go-connections/nat
RUN go get -d -v gopkg.in/yaml.v2

COPY . .
WORKDIR /go/src/github.com/kramergroup/vncd/cmd
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o vncd .

FROM scratch
COPY --from=builder /go/src/github.com/kramergroup/vncd/cmd/vncd /vncd
COPY assets/vncd.conf.yaml /etc/vncd.conf.yaml
ENTRYPOINT ["/vncd"]
