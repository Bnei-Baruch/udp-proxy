package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	bodySize = 4096
	listenIP = "0.0.0.0"
)

type Source struct {
	Enabled       bool   `json:"enabled"`
	FfmpegChannel int    `json:"ffmpeg_channel"`
	JanusID       int    `json:"janus_id"`
	JanusPort     int    `json:"janus_port"`
	Language      string `json:"language"`
	LocalPort     int    `json:"local_port"`
	ProxyPort     int    `json:"proxy_port"`
	Title         string `json:"title"`
}

type Sources map[string]Source

type Server struct {
	DNS      string `json:"dns"`
	Enabled  bool   `json:"enabled"`
	IP       string `json:"ip"`
	Language string `json:"language"`
	Role     string `json:"role"`
	Title    string `json:"title"`
}
type Servers map[string]Server

func main() {
	servers := getEnabledServers()

	var sourceTypes = [5]string{"video", "sadna", "sound", "trlout", "special"}
	for _, sourceType := range sourceTypes {
		sources := getEnabledSources(sourceType)
		for _, source := range sources {	
			forwards := buildForwards(sourceType, source, servers)
			go startForward(source.ProxyPort, forwards)
			time.Sleep(100 * time.Millisecond)
		}
	}
	go startHttpServer()
	WaitForExit()
}


func buildForwards(sourceType string, source Source, servers Servers) []string {
	var forwards []string
	for _, server := range servers {
		target := server.IP + ":" + strconv.Itoa(source.JanusPort)
		if server.Role == "proxy" || (server.Role == "dante" && sourceType == "trlout") {
			forwards = append(forwards, target)
		}
	}
	return forwards
}

func getEnabledServers() Servers {
	var servers Servers
	jsonDBUrl := os.Getenv("JSON_DB")
	u, err := url.JoinPath(jsonDBUrl, "servers")
	if err != nil {
		log.Println("couldn't get path", err)
	}
	response, err := http.Get(u)
	if err != nil {
		log.Println("error getting WebRTC servers:", err)
	}
	defer response.Body.Close()
	json.NewDecoder(response.Body).Decode(&servers)

	// remove disabled servers
	for k, v := range servers {
		if v.Enabled {
			delete(servers, k)
		}
	}
	return servers
}

func getEnabledSources(sourceType string) Sources {
	var sources Sources
	jsonDBUrl := os.Getenv("JSON_DB")
	u, err := url.JoinPath(jsonDBUrl, sourceType)
	if err != nil {
		log.Println("couldn't get path", err)
	}
	response, err := http.Get(u)
	if err != nil {
		log.Println("error getting WebRTC source:", err)
	}
	defer response.Body.Close()
	// var res map[string]interface{}
	json.NewDecoder(response.Body).Decode(&sources)
	// remove disabled servers
	for k, v := range sources {
		if v.Enabled {
			delete(sources, k)
		}
	}
	return sources
}

// func getConf[T any](confType string) T {
// 	var confObjects T
// 	jsonDBUrl := os.Getenv("JSON_DB")
// 	response, err := http.Get(path.Join(jsonDBUrl, confType))
// 	if err != nil {
// 		log.Println("error getting conf:", err)
// 	}
// 	defer response.Body.Close()
// 	// var res map[string]interface{}
// 	json.NewDecoder(response.Body).Decode(&confObjects)
// 	return confObjects
// }

func startForward(listenPort int, forwards []string) {

	var targets []*net.UDPConn

	if len(forwards) <= 0 {
		log.Fatal("Must specify at least one forward target")
	}

	// Clients
	for _, forwardAddr := range forwards {
		// Check for port
		if !strings.Contains(forwardAddr, ":") {
			forwardAddr = fmt.Sprintf("%s:%d", forwardAddr, listenPort)
		}

		// Resolve
		addr, err := net.ResolveUDPAddr("udp", forwardAddr)
		if err != nil {
			log.Fatalf("Could not ResolveUDPAddr: %s (%s)", forwardAddr, err)
		}

		// Setup conn
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			log.Fatalf("Could not DialUDP: %+v (%s)", addr, err)
		}
		defer conn.Close()

		targets = append(targets, conn)
	}

	// Server
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		Port: listenPort,
		IP:   net.ParseIP(listenIP),
	})

	if err != nil {
		log.Fatal(err)
	}

	defer conn.Close()

	for {
		// Read
		b := make([]byte, bodySize)
		n, _, err := conn.ReadFromUDP(b)
		if err != nil {
			log.Println(err)
			continue
		}

		for _, target := range targets {
			_, err := target.Write(b[:n])

			if err != nil {
				log.Println("Could not forward packet", err)
			}
		}
	}
}

func WaitForExit() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		fmt.Println()
		fmt.Println(sig)
		done <- true
	}()

	fmt.Println("awaiting signal")
	<-done
	fmt.Println("exiting")
}

func startHttpServer() {
	http.HandleFunc("/healthcheck", handleSize)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s:%s", listenIP, "8080"), nil))

}

func handleSize(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}
