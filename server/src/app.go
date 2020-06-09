package main

import (
    "encoding/json"
    "errors"
    "io"
    "fmt"
    "net/http"
    "log"
    "strings"

    "github.com/golang/gddo/httputil/header"
)

// loglevel log item
type LoglevelItem struct {
    Message string `json:"message"`
    Level   string `json:"level"`
    Logger  string `json:"logger"`
    Timestamp string `json:"timestamp"`
    Stacktrace string `json:"stacktrace"`
    Windowid string `json:"windowid"`
}

// loglevel logs (posted)
type LoglevelItems struct {
    Logs []LoglevelItem `json:"logs"`
}

// internal
type LogResponse struct {
    Message string
    Code int
}

type LogRequest struct {
    Appname string
    Token string
    Items []LoglevelItem
    Done  chan LogResponse
}

// all requests - FE to BE (sync)
var requests = make(chan LogRequest)

// config for a logger
type LoggerConfig struct {
    App string `json:"app"`
    Dir string `json:"dir"`
    Secret string `json:"secret"`
}

var debug = true

func main() {
    http.HandleFunc("/loglevel/", HandleLoglevelRequest)
    http.HandleFunc("/", HandleRootRequest)
    go requestHandler()
    log.Print("Running on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func HandleRootRequest(w http.ResponseWriter, r *http.Request) {
    ReturnError(w, r, "Not Found", http.StatusNotFound)
}
func ReturnError(w http.ResponseWriter, r *http.Request, message string, code int) {
    log.Printf("return error %d (%s) for %s %s\n", code, message, r.Method, r.URL.Path)
    http.Error(w, message, code)
}

// Request in Loglevel format
func HandleLoglevelRequest(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        ReturnError(w, r, "log accepts POST only", http.StatusMethodNotAllowed)
        return
    }
    mimetype, _ := header.ParseValueAndParams(r.Header, "Content-Type")
    if mimetype != "application/json" {
        ReturnError(w, r, "Send me JSON!", http.StatusUnsupportedMediaType)
        return
    }
    auth := r.Header.Get("Authorization")
    if len(auth) < 7 || auth[0:7] != "Bearer " {
        ReturnError(w, r, "Missing/non-bearer authorization", http.StatusUnauthorized)
        return
    }
    authtoken := auth[7:]
    appname := r.URL.Path[10:] // /loglevel/...
    slix := strings.Index(appname,"/")
    if slix > -1 || len(appname) == 0 {
        ReturnError(w, r, "Invalid/missing app name", http.StatusNotFound)
        return
    }
    if debug {
        log.Printf("POST %s (token %s)", appname, authtoken)
    }
    // with help from https://www.alexedwards.net/blog/how-to-properly-parse-a-json-request-body
    // limit size = 10MB
    r.Body = http.MaxBytesReader(w, r.Body, 1048576*10) 
    dec := json.NewDecoder(r.Body)
    // disallow additional fields
    dec.DisallowUnknownFields()

    var ls LoglevelItems
    err := dec.Decode(&ls)
    if err != nil {
        var syntaxError *json.SyntaxError
        var unmarshalTypeError *json.UnmarshalTypeError

        switch {
        // Catch any syntax errors in the JSON
        case errors.As(err, &syntaxError):
            ReturnError(w, r, "badly formed JSON", http.StatusBadRequest)

        // In some circumstances Decode() may also return an
        // io.ErrUnexpectedEOF error for syntax errors in the JSON. There
        // is an open issue regarding this at
        // https://github.com/golang/go/issues/25956.
        case errors.Is(err, io.ErrUnexpectedEOF):
            ReturnError(w, r, "badly formed JSON", http.StatusBadRequest)

        // Catch any type errors
        case errors.As(err, &unmarshalTypeError):
            ReturnError(w, r, "JSON type error", http.StatusBadRequest)

        // Catch the error caused by extra unexpected fields in the request
        // body. 
        case strings.HasPrefix(err.Error(), "json: unknown field "):
            ReturnError(w, r, "JSON with unknown fields", http.StatusBadRequest)

        // An io.EOF error is returned by Decode() if the request body is
        // empty.
        case errors.Is(err, io.EOF):
            ReturnError(w, r, "Empty request", http.StatusBadRequest)

        // Catch the error caused by the request body being too large. Again
        // there is an open issue regarding turning this into a sentinel
        // error at https://github.com/golang/go/issues/30715.
        case err.Error() == "http: request body too large":
            ReturnError(w, r, "Request too large", http.StatusRequestEntityTooLarge)

        // Otherwise default to logging the error and sending a 500 Internal
        // Server Error response.
        default:
            log.Println(err.Error())
            ReturnError(w, r, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
        }
        return
    }

    // Call decode again, using a pointer to an empty anonymous struct as 
    // the destination. If the request body only contained a single JSON 
    // object this will return an io.EOF error. So if we get anything else, 
    // we know that there is additional data in the request body.
    err = dec.Decode(&struct{}{})
    if err != io.EOF {
        ReturnError(w, r, "Extra data after body", http.StatusBadRequest)
        return
    }

    // reply channel - message and http status
    done := make(chan LogResponse)
    req := LogRequest{
         Appname: appname,
         Token: authtoken,
         Items: ls.Logs,
         Done: done,
    }
    requests <- req
    res := <-done
    if res.Code == http.StatusOK {
        fmt.Fprint(w, res.Message);
    } else {
        ReturnError(w, r, res.Message, res.Code)
    }
}

// Logger type / internal data
type Logger struct{
    Appname string
    Exists bool
    Token string
    // TODO
    Requests chan LogRequest
}

// call only once as go routine!
func requestHandler() {
    loggers := make(map[string]*Logger)

    for true  {
        req := <-requests

        logger := loggers[req.Appname]
        if logger == nil {
            log.Printf("Create logger %s\n", req.Appname)
            logger = new(Logger)
            logger.Appname = req.Appname
            logger.Exists = true // TODO
            logger.Requests = make(chan LogRequest)
            go loggerHandler(logger)
            loggers[req.Appname] = logger
        }
        logger.Requests <- req
    }
}
// one per logger only
func loggerHandler(logger *Logger) {
    for true {
        req := <-logger.Requests

        log.Printf("Log %s: %d items\n", req.Appname, len(req.Items))

        // TODO
        req.Done <- LogResponse{
            Message:"OK",
            Code: http.StatusOK,
        }
    }
}

