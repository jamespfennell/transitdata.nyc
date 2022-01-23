FROM golang:1.16 AS builder

WORKDIR /subwaydata.nyc

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY . ./

RUN go build -o /usr/bin/website website/website.go
RUN go build -o /usr/bin/etl etl/etl.go

RUN go test ./etl/git

# We use this buildpack image because it already has SSL certificates installed
FROM buildpack-deps:buster-curl

COPY --from=builder /usr/bin/website /usr/bin
COPY --from=builder /usr/bin/etl /usr/bin

ENTRYPOINT ["website"]
