FROM golang:1.16 as base
RUN apk add --no-cache libstdc++ gcc g++ make git ca-certificates linux-headers
WORKDIR /go/src/github.com/dodoex/gethexporter
ADD . .
RUN go mod tidy && go install

FROM alpine:latest
RUN apk add --no-cache jq ca-certificates linux-headers
COPY --from=base /go/bin/gethexporter /usr/local/bin/gethexporter
ENV GETH https://mainnet.infura.io/v3/f5951d9239964e62aa32ca40bad376a6
ENV ADDRESSES ""
ENV PREFIX ""
ENV DELAY 500

EXPOSE 9090
ENTRYPOINT gethexporter
