package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/savsgio/atreugo/v11"
	"gopkg.in/yaml.v2"
)

type Route struct {
	Method string `yaml:"method"`
	Route  string `yaml:"route"`
	File   string `yaml:"file"`
	Data   string `yaml:"data"`
}

var (
	routes     []Route
	routesLock sync.Mutex
	server     *atreugo.Atreugo
	gdata      map[string]string
	gpath      map[string]string
	port       string
)

func setupServer() {
	server = nil
	config := atreugo.Config{Addr: fmt.Sprintf("0.0.0.0:%s", port)}
	server = atreugo.New(config)

	routesLock.Lock()
	for _, route := range routes {
		handler := createHandler(route.Route, route.File, route.Data, route.Method)
		server.Router.Path(route.Method, route.Route, handler)
	}
	routesLock.Unlock()

	go func() {
		if err := server.ListenAndServe(); err != nil {
			panic(err)
		}
	}()
}

func main() {
	gdata = make(map[string]string)
	gpath = make(map[string]string)

	// Read YAML file path from environment variable
	routesFile := os.Getenv("ROUTES_FILE")
	if routesFile == "" {
		routesFile = "./routes.yaml"
	}

	port = os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	initialRoutes, err := readYAMLFile(routesFile)
	if err != nil {
		log.Fatal(err)
	}

	updateRoutes(initialRoutes)

	go watchRoutesFile(routesFile)
	setupServer()

	select {}
}

func readYAMLFile(filename string) ([]Route, error) {
	// Read the YAML file
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file: %v", err)
	}

	// Unmarshal YAML into Route objects
	var newRoutes []Route
	err = yaml.Unmarshal(data, &newRoutes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %v", err)
	}

	return newRoutes, nil
}

func watchRoutesFile(filename string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	err = watcher.Add(filename)
	if err != nil {
		log.Printf("failed to watch file: %v", err)
		return
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Println("Routes file modified. Reloading routes...")

				newRoutes, err := readYAMLFile(filename)
				if err != nil {
					log.Printf("failed to reload routes: %v", err)
					continue
				}

				updateRoutes(newRoutes)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}

			log.Printf("error watching routes file: %v", err)
		}
	}
}

func getIdx(route string, method string) string {
	return fmt.Sprintf("%s:%s", route, method)
}

func updateRoutes(newRoutes []Route) {
	routesLock.Lock()
	routes = newRoutes
	routesLock.Unlock()

	if server != nil {
		for _, route := range routes {
			gdata[getIdx(route.Route, route.Method)] = route.Data
			gpath[getIdx(route.Route, route.Method)] = route.File
		}
	}
}

func createHandler(route string, filename string, data string, method string) atreugo.View {
	gdata[getIdx(route, method)] = data
	gpath[getIdx(route, method)] = filename

	return func(ctx *atreugo.RequestCtx) error {
		ctx.Response.Header.SetContentType("application/json")
		ctx.Response.Header.SetStatusCode(200)

		if gpath[getIdx(route, method)] == "" {
			if gdata[getIdx(route, method)] == "" {
				ctx.Response.Header.SetStatusCode(400)
				ctx.SetBodyString("{\"error\": \"no data nor file specified\"}")
				return nil
			}

			ctx.Response.SetBodyString(string(gdata[getIdx(route, method)]))
			return nil
		} else {
			fileData, err := ioutil.ReadFile(gpath[getIdx(route, method)])
			if err != nil {
				ctx.Response.Header.SetStatusCode(400)
				ctx.Response.SetBodyString("{\"error\": \"not able to read the file\"}")
				return nil
			}

			ctx.Response.SetBodyString(string(fileData))
			return nil
		}
	}
}
