FROM golang:1.25-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY go.mod ./
COPY *.go ./
COPY web ./web
RUN go test ./... && go build -trimpath -ldflags="-s -w" -o /out/clash-acl .
RUN mkdir -p /runtime/data && chmod 700 /runtime/data && chown 10001:10001 /runtime/data

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=10001:10001 /runtime/ /
COPY --from=build /out/clash-acl /clash-acl
USER 10001:10001
ENV CLASH_ACL_LISTEN_ADDR=0.0.0.0:8080 CLASH_ACL_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/clash-acl"]
