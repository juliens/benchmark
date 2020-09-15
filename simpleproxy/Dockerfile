FROM golang:1.15-alpine as builder

ADD ./ /proxy

WORKDIR /proxy

ENV CGO_ENABLED=0
RUN go build -o proxy .

FROM alpine
COPY --from=builder /proxy/proxy /proxy

ENTRYPOINT [ "/proxy" ]
