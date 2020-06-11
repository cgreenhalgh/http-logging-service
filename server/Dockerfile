FROM golang:1.14-alpine

WORKDIR /go/src/app

RUN apk --update add git openssh && \
    rm -rf /var/lib/apt/lists/* && \
    rm /var/cache/apk/*

COPY src src
RUN go get -d -v ./...

RUN go build -v -o app ./src/app.go

VOLUME /go/src/app/logs
VOLUME /go/src/app/conf

EXPOSE 8080
CMD ["./app"]
#CMD ["sleep","100000000"]
