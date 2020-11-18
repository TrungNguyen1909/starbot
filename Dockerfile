FROM golang:alpine as build-env

WORKDIR /go/src/github.com/TrungNguyen1909/starbot

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build --ldflags "-w -s" -v -o /bin/starbot main.go

FROM alpine
COPY --from=build-env /bin/starbot /bin/starbot
ENTRYPOINT ["/bin/starbot"]