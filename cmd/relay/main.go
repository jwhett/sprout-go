package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"time"

	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/grove"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
	"git.sr.ht/~whereswaldon/sprout-go/watch"
)

func tickerChan(seconds int) <-chan time.Time {
	return time.NewTicker(time.Second * time.Duration(seconds)).C
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	wd, _ := os.Getwd()
	grovePath := flag.String("grovepath", wd, "Location of the grove of Arbor Nodes to use")
	certpath := flag.String("certpath", "", "Location of the TLS public key (certificate file)")
	keypath := flag.String("keypath", "", "Location of the TLS private key (key file)")
	insecure := flag.Bool("insecure", false, "Don't verify the TLS certificates of addresses provided as arguments")
	selftest := flag.Bool("selftest", false, "Dial yourself to verify that basic connection handling is working")
	tlsPort := flag.Int("tls-port", 7777, "TLS listen port")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			`Usage:

%s [flags] [address ...]

%s acts as a Sprout relay. It will listen on the port configured by its flags
and will establish Sprout connections to all addresses provided as arguments.

`, os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	cert, err := tls.LoadX509KeyPair(*certpath, *keypath)
	if err != nil {
		log.Fatalf("Failed loading certs: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	tlsConfig.BuildNameToCertificate()

	address := fmt.Sprintf(":%d", *tlsPort)
	listener, err := tls.Listen("tcp", address, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to start TLS listener on address %s: %v", address, err)
	}
	done := make(chan struct{})

	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	grove, err := grove.New(*grovePath)
	if err != nil {
		log.Fatalf("Failed to create grove at %s: %v", *grovePath, err)
	}
	messages := sprout.NewSubscriberStore(grove)
	defer messages.Destroy()

	watchLogger := log.New(log.Writer(), "watch", log.LstdFlags|log.Lshortfile)
	_, err = watch.Watch(*grovePath, watchLogger, func(filename string) {
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			watchLogger.Printf("Failed reading watched file: %s: %v", filename, err)
			return
		}
		node, err := forest.UnmarshalBinaryNode(data)
		if err != nil {
			watchLogger.Printf("Failed unmarshalling watched file: %s: %v", filename, err)
			return
		}
		if err := node.ValidateDeep(messages); err != nil {
			watchLogger.Printf("Failed validating node from watched file: %s: %v", filename, err)
			return

		}
		if err := messages.Add(node); err != nil {
			watchLogger.Printf("Failed adding node from watched file: %s: %v", filename, err)
			return
		}
	})

	// start listening for new connections
	go func() {
		workerCount := 0
		for {
			log.Printf("Waiting for connections...")
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed accepting connection: %v", err)
				continue
			}
			worker, err := sprout.NewWorker(done, conn, messages)
			if err != nil {
				log.Printf("Failed launching worker: %v", err)
				continue
			}
			worker.Logger = log.New(log.Writer(), fmt.Sprintf("worker-%d ", workerCount), log.Flags())
			go worker.Run()
			log.Printf("Launched worker-%d to handle new connection", workerCount)
			workerCount++
			select {
			case <-done:
				log.Printf("Done channel closed")
				return
			default:
			}
		}
	}()

	// connect to ourselves as a test if requested
	if *selftest {
		go func() {
			time.Sleep(time.Second)
			log.Printf("Launching test connection to verify basic functionality")
			conn, err := tls.Dial("tcp", address, &tls.Config{
				InsecureSkipVerify: true,
			})
			if err != nil {
				log.Printf("Test dial failed: %v", err)
				return
			}
			defer func() {
				if err := conn.Close(); err != nil {
					log.Printf("Failed to close test connection: %v", err)
					return
				}
				log.Printf("Closed test connection")
			}()
			sconn, err := sprout.NewConn(conn)
			if err != nil {
				log.Printf("Failed to create sprout conn from test dial: %v", err)
			}
			log.Printf("Sending version information on test connection")
			if err := sconn.SendVersion(time.NewTicker(time.Second * 5).C); err != nil {
				log.Printf("Failed to send version information from test conn: %v", err)
			}
		}()
	}

	// dial peers mentioned in arguments
	for _, address := range flag.Args() {
		var tlsConfig *tls.Config
		if *insecure {
			tlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		conn, err := tls.Dial("tcp", address, tlsConfig)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", address, err)
			continue
		}
		worker, err := sprout.NewWorker(done, conn, messages)
		if err != nil {
			log.Printf("Failed launching worker to connect to address %s: %v", address, err)
			continue
		}
		worker.Logger = log.New(log.Writer(), fmt.Sprintf("worker-%v ", address), log.Flags())
		go worker.Run()
		go worker.BootstrapLocalStore(1024, time.Second*5)
	}

	// Block until a signal is received.
	<-c
	close(done)
}
