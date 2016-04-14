package watchserverfile

import (
	"log"
	"net"
	"net/http"

	"gopkg.in/fsnotify.v1"
)

// ReloadHandlerFunc is the function signature that is used for functions that
// will do the reloading of handlers
type ReloadHandlerFunc func(watch *Server)

// Server is the stuct that is composed of the http.Server. It also
// holds the reload function and the channels that are used for signalling.
type Server struct {
	ReloadFile chan string

	reloadFunc  ReloadHandlerFunc
	reloadServe chan struct{}
	http.Server
}

// Handler is the function that is used to set the http.Server handler
// when it needs to be reset
func (ws *Server) Handler(handler http.Handler) {
	ws.Server.Handler = handler
}

// ListenAndServe is the main function that is called. The main difference is
// that instead of passing a http.Handler function, you will be passing a
// ReloadHandlerFunc that will be a function that will reload the Handler as
// it is needed.
func (ws *Server) ListenAndServe(address string, reloadFunc ReloadHandlerFunc) error {
	ws.Addr = address
	ws.reloadFunc = reloadFunc

	// now wait for the other times when we needed to
	go func() {
		for {
			// clear the handler
			ws.Handler(nil)
			ws.reloadFunc(ws)
			ws.reloadServe <- struct{}{} // reset the listening binding
		}
	}()

	ws.reloadFunc(ws)
	return ws.listenAndServe()
}

// listenAndServe is the function that would be most like a Mux
// listen and serve function. It has a channel that does the blocking
// and not the underlining Serve() function. This is so that the channel
// can be unblocked to reset the handlers for the Serve() then then
// the connection can be re-established.
func (ws *Server) listenAndServe() error {
	addr := ws.Addr
	if addr == "" {
		addr = ":http"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	for {
		l := ln.(*net.TCPListener)
		defer l.Close()
		go func(l net.Listener) {
			log.Println("Listening and serving", addr, "...")
			ws.Serve(l)
		}(l)
		<-ws.reloadServe
	}
}

// watchFile is the internal function that will grab the notifiy events
// and then pass along the reloading of the file.
func (ws *Server) watchFile(watcher *fsnotify.Watcher) {
	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Println("Reloading the file...")
				ws.ReloadFile <- event.Name
			}
		case err := <-watcher.Errors:
			log.Println("Watcher Error:", err)
		}
	}
}

// New accepts a list of file names that can be watch, it will
// then return a new object that can be used kinda like the
// http.Server object.
func New(filename string) *Server {
	ws := new(Server)
	ws.ReloadFile = make(chan string, 1)
	ws.reloadServe = make(chan struct{}, 1)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("Watcher: ", err)
	}

	err = watcher.Add(filename)
	if err != nil {
		log.Fatal("New WatcherServer: ", err)
	}

	ws.ReloadFile <- filename
	go ws.watchFile(watcher)

	return ws
}
