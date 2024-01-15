FROM golang:1.21.5-bullseye AS builder
WORKDIR /go/src/github.com/kluster-manager/cluster-auth
COPY . .
RUN go env
RUN make build-manager
RUN make build-agent

FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
COPY --from=builder /go/src/github.com/kluster-manager/cluster-auth/bin/manager /
COPY --from=builder /go/src/github.com/kluster-manager/cluster-auth/bin/agent /
