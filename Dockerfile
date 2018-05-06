FROM golang:latest AS builder
WORKDIR /go/src/github.com/kramergroup/vncd
RUN go get -d -v github.com/docker/docker/api \
                 github.com/docker/docker/client \
                 github.com/docker/go-connections/nat \
                 gopkg.in/yaml.v2 \
                 k8s.io/client-go/kubernetes \
                 k8s.io/client-go/rest \
                 k8s.io/client-go/tools/clientcmd \
                 k8s.io/apimachinery/pkg/apis/meta/v1 \
                 k8s.io/api/core/v1 \
                 golang.org/x/net/websocket && \
    rm -rf /go/src/github.com/docker/docker/vendor/github.com/docker/go-connections/nat

COPY . .
WORKDIR /go/src/github.com/kramergroup/vncd/cmd
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o vncd .

FROM scratch
COPY --from=builder /go/src/github.com/kramergroup/vncd/cmd/vncd /vncd
COPY assets/vncd.conf.yaml /etc/vncd/vncd.conf.yaml
ENTRYPOINT ["/vncd"]
