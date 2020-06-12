package main

import (
    "encoding/json"
    "errors"
    "io"
    "io/ioutil"
    "fmt"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

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
    ServerTime string `json:"servertime"`
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

var debug = false
var logdir string
var confdir string
const LOGPATH = "logs"
const CONFPATH = "conf"

func main() {
    cwd, err := os.Getwd()
    if err != nil {
        log.Fatal(err)
    }
    logdir = filepath.Join(cwd, LOGPATH)
    confdir = filepath.Join(cwd, CONFPATH)
    _, err = ioutil.ReadDir(logdir)
    if err != nil {
        log.Fatalf("Cannot read log dir %s: %s", logdir, err)
    }
    _,err = ioutil.ReadDir(confdir)
    if err != nil {
        log.Fatalf("Cannot read config dir %s: %s", confdir, err)
    }
    log.Printf("Log to %s, config in %s", logdir, confdir)

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
    // limit size = 10MB
    r.Body = http.MaxBytesReader(w, r.Body, 1048576*10)

    message,code := getLoglevelResponse(r)
    if code == http.StatusOK {
        fmt.Fprint(w, message);
    } else {
        ReturnError(w, r, message, code)
    }
}
func getLoglevelResponse(r *http.Request) (string,int) {
    if r.Method != http.MethodPost {
        return "log accepts POST only", http.StatusMethodNotAllowed
    }
    mimetype, _ := header.ParseValueAndParams(r.Header, "Content-Type")
    if mimetype != "application/json" {
        return "Send me JSON!", http.StatusUnsupportedMediaType
    }
    auth := r.Header.Get("Authorization")
    if len(auth) < 7 || auth[0:7] != "Bearer " {
        return "Missing/non-bearer authorization", http.StatusUnauthorized
    }
    authtoken := auth[7:]
    appname := r.URL.Path[10:] // /loglevel/...
    slix := strings.Index(appname,"/")
    if slix > -1 || len(appname) == 0 {
        return "Invalid/missing app name", http.StatusNotFound
    }
    if debug {
        log.Printf("POST %s (token %s)", appname, authtoken)
    }
    // with help from https://www.alexedwards.net/blog/how-to-properly-parse-a-json-request-body
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
            return "badly formed JSON", http.StatusBadRequest

        // In some circumstances Decode() may also return an
        // io.ErrUnexpectedEOF error for syntax errors in the JSON. There
        // is an open issue regarding this at
        // https://github.com/golang/go/issues/25956.
        case errors.Is(err, io.ErrUnexpectedEOF):
            return "badly formed JSON", http.StatusBadRequest

        // Catch any type errors
        case errors.As(err, &unmarshalTypeError):
            return "JSON type error", http.StatusBadRequest

        // Catch the error caused by extra unexpected fields in the request
        // body. 
        case strings.HasPrefix(err.Error(), "json: unknown field "):
            return "JSON with unknown fields", http.StatusBadRequest

        // An io.EOF error is returned by Decode() if the request body is
        // empty.
        case errors.Is(err, io.EOF):
            return "Empty request", http.StatusBadRequest

        // Catch the error caused by the request body being too large. Again
        // there is an open issue regarding turning this into a sentinel
        // error at https://github.com/golang/go/issues/30715.
        case err.Error() == "http: request body too large":
            return "Request too large", http.StatusRequestEntityTooLarge

        // Otherwise default to logging the error and sending a 500 Internal
        // Server Error response.
        default:
            log.Println(err.Error())
            return http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError
        }
    }

    // Call decode again, using a pointer to an empty anonymous struct as 
    // the destination. If the request body only contained a single JSON 
    // object this will return an io.EOF error. So if we get anything else, 
    // we know that there is additional data in the request body.
    err = dec.Decode(&struct{}{})
    if err != io.EOF {
        return "Extra data after body", http.StatusBadRequest
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
    return res.Message, res.Code
}

// Logger type / internal data
type Logger struct{
    Appname string
    Token string
    Requests chan LogRequest
    Configured bool
    ConfigLastCheck time.Time
    ConfigFile string
    Config LoggerConfig
    Logdir string
    CreateLast time.Time
    WriteLast time.Time
    NeedsFlush bool
    LogFile *os.File
}
// don't force dispatch thread to wait for back-end logger thread
// (most of the time)
const REQUEST_BUFFER_SIZE = 100

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
            logger.Requests = make(chan LogRequest, REQUEST_BUFFER_SIZE)
            go loggerHandler(logger)
            loggers[req.Appname] = logger
        }
        logger.Requests <- req
    }
}

// 1 minute
const CACHE_CONFIG_HOURS = 1.0/60 // 1 minute
const FLUSH_HOURS = 1.0/60/2 // 30 seconds
const ROTATE_HOURS = 24.0
// RFC3339 with ms accuracy
const RFC3339MS = "2006-01-02T15:04:05.000Z07:00"

// one per logger only
func loggerHandler(logger *Logger) {
    for true {
        // TODO call HandleRequest after a time with no items to force sync/close
        req := <-logger.Requests

        if debug {
            log.Printf("Log %s: %d items\n", req.Appname, len(req.Items))
        }
        msg,code := logger.HandleRequest(req)
        req.Done <- LogResponse{
            Message:msg,
            Code: code,
        }
    }
}
func (this *Logger) HandleRequest(req LogRequest) (string,int) {
    now := time.Now()
    // read or update Config; check Logdir exists
    if this.ConfigLastCheck.IsZero() ||
       now.Sub(this.ConfigLastCheck).Hours() > CACHE_CONFIG_HOURS {
        this.ConfigFile = filepath.Join(confdir, req.Appname+".json")
        if debug {
            log.Printf("(Re)Read config %s", this.ConfigFile)
        }
        this.ConfigLastCheck = now
        rawconfig,err := ioutil.ReadFile(this.ConfigFile)
        if err != nil {
            log.Printf("Error reading config %s for %s: %s", this.ConfigFile, req.Appname, err)
            this.Configured = false
        } else {
            var newConfig LoggerConfig
            err = json.Unmarshal(rawconfig, &newConfig)
            if err != nil {
                log.Printf("Error parsing config %s for %s: %s", this.ConfigFile, req.Appname, err)
                this.Configured = false
            } else {
                if debug {
                    log.Printf("Config for %s: dir %s", req.Appname, newConfig.Dir)
                }
                if newConfig.Dir == "" {
                    newConfig.Dir = req.Appname
                }
                this.Configured = true
                if newConfig.Dir != this.Config.Dir {
                    // close
                    this.CloseLogFile()
                    log.Printf("Set log dir %s for %s", newConfig.Dir, req.Appname)
                    this.Logdir =  filepath.Join(logdir, newConfig.Dir)
                    linfo,err := os.Stat(this.Logdir)
                    if err != nil && os.IsNotExist(err) {
                        err = os.Mkdir(this.Logdir, 0775)
                        if err != nil {
                            log.Printf("Could not create new log dir %s for %s: %s", this.Logdir, req.Appname, err)
                            this.Configured = false
                        }
                        log.Printf("Created log dir %s for %s", this.Logdir, req.Appname)
                    } else if err != nil {
                        log.Printf("Problem with log dir %s for %s: %s", this.Logdir, req.Appname, err)
                        this.Configured = false
                    } else if ! linfo.IsDir() {
                        log.Printf("Log dir %s for %s is not a directory", this.Logdir, req.Appname)
                        this.Configured = false
                    }
                }
                this.Config = newConfig
            }
        }
    }
    if  ! this.Configured {
        return "Logger not configured", http.StatusNotFound
    }
    if req.Token != this.Config.Secret {
        log.Printf("invalid token for log %s", req.Appname)
        return "Invalid token", http.StatusUnauthorized
    }
    if this.LogFile != nil && now.Sub(this.CreateLast).Hours() > ROTATE_HOURS {
        this.CloseLogFile()
    }
    if this.LogFile != nil && this.NeedsFlush && now.Sub(this.WriteLast).Hours() > FLUSH_HOURS {
        err := this.LogFile.Sync()
        if err != nil {
            // trigger reopen
            this.CloseLogFile()
        }
        this.NeedsFlush = false
    }
    // null write
    if len(req.Items) == 0 {
        return "OK",http.StatusOK
    }

    if this.LogFile == nil {
        filename := now.UTC().Format(time.RFC3339) + ".log"
        path := filepath.Join(this.Logdir, filename)
        log.Printf("New log file %s for %s", path, this.Appname)
        file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0755)
        if err != nil {
            log.Printf("Error opening log %s: %s", path, err)
            this.LogFile = nil
            return "Could not create logfile", http.StatusInternalServerError
        }
        this.LogFile = file
        this.CreateLast = now
    }
    for i:= 0; i<len( req.Items ); i++ {
        req.Items[i].ServerTime = now.UTC().Format(RFC3339MS)
        buf,err := json.Marshal( req.Items[i] )
        if err != nil {
            log.Printf("Error marshalling log item: %s", err)
            return "Error marshalling log item", http.StatusInternalServerError
        }
        _,err = this.LogFile.Write(buf)
       if err != nil {
           log.Printf("Error writing log item: %s", err)
           this.CloseLogFile()
           return "Error writing log item", http.StatusInternalServerError
       }

       _,_ = this.LogFile.Write([]byte("\n"))
    }
    if ! this.NeedsFlush {
        this.NeedsFlush = true
        this.WriteLast = now
    }
    return "OK",http.StatusOK
}

func (this *Logger) CloseLogFile() {
    if this.LogFile != nil {
        err := this.LogFile.Sync()
        if err != nil {
            log.Printf("Error syncing logfile for %s: %s", this.Appname, err)
        }
        err = this.LogFile.Close()
        if err != nil {
            log.Printf("Error closing logfile for %s: %s", this.Appname, err)
        }
        this.LogFile = nil
    }
}

