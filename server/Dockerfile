FROM golang:1.14-alpine

WORKDIR /go/src/app
COPY . .

RUN apk --update add git openssh && \
    rm -rf /var/lib/apt/lists/* && \
    rm /var/cache/apk/*

RUN go get -d -v ./...
#RUN go install -v ./...

#RUN GGO_ENABLED=0 GOOS=linux go build -a -tags netgo -installsuffix netgo -ldflags '-s -w' -o app /src/app.go
RUN go build -v -o app ./src/app.go

EXPOSE 8080
#CMD ["app"]
CMD ["sleep","100000000"]
