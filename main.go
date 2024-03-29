package main

import (
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"gopkg.in/alecthomas/kingpin.v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	debug    = kingpin.Flag("debug", "Enable debug mode").Envar("DEBUG").Bool()
	listenIP = kingpin.Flag("listen-ip", "IP to listen in").Default("0.0.0.0").Envar("LISTEN_IP").IP()
	bodySize = kingpin.Flag("body-size", "Size of body to read").Default("4096").Envar("BODY_SIZE").Int()
	pretty   = kingpin.Flag("pretty", "").Default("true").Envar("PRETTY").Hidden().Bool()
)

func main() {
	err, servers := getConf("servers")
	if err != nil {
		fmt.Println("get conf:", err)
	}

	var sources = [5]string{"video", "sadna", "sound", "trlout", "special"}

	for _, source := range sources {
		err, conf := getConf(source)
		if err != nil {
			fmt.Println("get conf:", err)
		}

		fmt.Println("-------", source)

		for _, v := range conf {
			var forwards []string
			src := v.(map[string]interface{})
			ProxyPort := int(src["proxy_port"].(float64))
			JanusPort := fmt.Sprint(src["janus_port"].(float64))
			SrcEnabled := src["enabled"].(bool)

			if SrcEnabled {
				for _, s := range servers {
					srv := s.(map[string]interface{})
					ip := srv["ip"].(string)
					role := srv["role"].(string)
					SrvEnabled := srv["enabled"].(bool)
					if role == "proxy" && SrvEnabled {
						forwards = append(forwards, ip+":"+JanusPort)
					}
					if source == "trlout" && role == "dante" && SrvEnabled {
						forwards = append(forwards, ip+":"+JanusPort)
					}
				}

				go startForward(ProxyPort, forwards)
			}

			time.Sleep(100000000)
		}
	}

	WaitForExit()
}

func getConf(source string) (error, map[string]interface{}) {
	response, err := http.Get(os.Getenv("JSON_DB") + source)
	if err != nil {
		return err, nil
	}
	defer response.Body.Close()
	var res map[string]interface{}
	json.NewDecoder(response.Body).Decode(&res)
	return nil, res
}

func startForward(listenPort int, forwards []string) {

	var targets []*net.UDPConn
	// CLI
	kingpin.Parse()

	// Log setup
	if *debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if !*pretty {
		log.SetFormatter(&log.TextFormatter{
			DisableColors: true,
			FullTimestamp: true,
		})
	}

	if len(forwards) <= 0 {
		log.Fatal("Must specify at least one forward target")
	}

	// Clients
	for _, forward := range forwards {
		// Check for port
		if strings.Index(forward, ":") < 0 {
			forward = fmt.Sprintf("%s:%d", forward, listenPort)
		}

		// Resolve
		addr, err := net.ResolveUDPAddr("udp", forward)
		if err != nil {
			log.Fatalf("Could not ResolveUDPAddr: %s (%s)", forward, err)
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
		IP:   *listenIP,
	})
	if err != nil {
		log.Fatal(err)
	}

	defer conn.Close()

	// Startup status
	log.WithFields(log.Fields{
		"ip":   *listenIP,
		"port": listenPort,
	}).Infof("Server started")
	for i, target := range targets {
		log.WithFields(log.Fields{
			"num":   i + 1,
			"total": len(targets),
			"addr":  target.RemoteAddr(),
		}).Info("Forwarding target configured")
	}

	for {
		// Read
		b := make([]byte, *bodySize)
		n, addr, err := conn.ReadFromUDP(b)
		if err != nil {
			log.Error(err)
			continue
		}

		// Log receive
		ctxLog := log.WithFields(log.Fields{
			"source": addr.String(),
			"body":   string(b[:n]),
		})
		ctxLog.Debugf("Recieved packet")

		// Proxy
		for _, target := range targets {
			_, err := target.Write(b[:n])

			// Log proxy
			ctxLog := ctxLog.WithFields(log.Fields{
				"target": target.RemoteAddr(),
			})

			if err != nil {
				ctxLog.Warn("Could not forward packet", err)
			} else {
				ctxLog.Debug("Wrote to target")
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
