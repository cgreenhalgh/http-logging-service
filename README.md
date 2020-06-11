# HTTP Logging Service

Chris Greenhalgh, The University of Nottingham

plan:

- golang http server writing logs

- javascript client - 
[loglevel](https://github.com/pimterry/loglevel) and 
[loglevel-plugin-remote](https://github.com/kutuluk/loglevel-plugin-remote)

status:
- v1

## config - server

server config directory

contains config files

file json encoded object, called `APPNAME.json`:
- app: string (optional)
- dir: string (optional, defaults to APPNAME)
- secret: string (required)

## data format

POST application/json

loglevel-plugin-remote:

posts {"logs":[...]}

where each log item is object with:
- `message` string, interpolated, may embed json-encoded objects (%j)
- `level` string? 'trace', 'error', 'warn', ...
- `logger` string (default '')
- `timestamp` string, e.g. "2017-05-29T12:53:46.000Z"
- `stacktrace` string (default '')

need to add client id, say 'windowid'

server adds 'servertime' (in RFC3339 format)

## client

Use 
[loglevel](https://github.com/pimterry/loglevel) and
[loglevel-plugin-remote](https://github.com/kutuluk/loglevel-plugin-remote)

For remote.appy use options:
```
  format: format,
  url: "/loglevel/APPLICATION-NAME",
  token: "INSERT-BEARER-TOKEN-FOR-LOGGER"
```

For sample client/code see [testclient/testloglevel.html](testclient/testloglevel.html).


## Build

Server

```
sudo docker build -t logging-server server
```

dev - if you have "internal" network
```
sudo docker run --rm -d -p 8080:8080 --name=logging-server \
 -v `pwd`/server:/go/src/app \
 --network=internal \
 logging-server
sudo docker exec -it logging-server /bin/sh
```
test
```
curl -v -X POST -H "Authorization: Bearer 123" \
  -H "Content-type: application/json" \
  --data '{"logs":[{"message":"hello"}]}' \
  http://172.18.0.3:8080/loglevel/hello-v http://172.18.0.3:8080/HELLO
```

