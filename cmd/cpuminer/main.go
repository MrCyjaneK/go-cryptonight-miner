package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/alecthomas/kingpin"
	stratum "github.com/gurupras/go-stratum-client"
	cpuminer "github.com/gurupras/go-cryptonite-miner/cpu-miner"
	"github.com/gurupras/go-cryptonite-miner/miner"
	colorable "github.com/mattn/go-colorable"
	log "github.com/sirupsen/logrus"
)

var (
	app        = kingpin.New("cpuminer", "CPU Cryptonight miner")
	url        = app.Flag("url", "URL of the pool").Short('o').Required().String()
	username   = app.Flag("username", "Username (usually the wallet address)").Short('u').Required().String()
	password   = app.Flag("password", "Password").Short('p').Default("x").String()
	threads    = app.Flag("threads", "Number of threads to run").Short('t').Default(fmt.Sprintf("%d", runtime.NumCPU())).Int()
	cpuprofile = app.Flag("cpuprofile", "Run CPU profiler").String()
)

func main() {
	kingpin.MustParse(app.Parse(os.Args[1:]))

	if runtime.GOOS == "windows" {
		log.SetFormatter(&log.TextFormatter{ForceColors: true})
		log.SetOutput(colorable.NewColorableStdout())
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatalf("Failed to create cpuprofile file: %v", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("Failed to start CPU profile: %v", err)
		}
		log.Infof("Starting CPU profiling")
		defer pprof.StopCPUProfile()
	}

	sc := stratum.New()

	hashrateChan := make(chan *miner.HashRate, 10)
	go miner.SetupHashRateLogger(hashrateChan)

	numMiners := *threads
	miners := make([]miner.Interface, numMiners)
	for i := 0; i < numMiners; i++ {
		miner := cpuminer.NewXMRigCPUMiner(sc)
		miner.RegisterHashrateListener(hashrateChan)
		miners[i] = miner
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	for i := 0; i < numMiners; i++ {
		go miners[i].Run()
	}

	// responseChan := make(chan *stratum.Response)
	//
	// sc.RegisterResponseListener(responseChan)

	if err := sc.Connect(*url); err != nil {
		log.Fatalf("Failed to connect to url :%v  - %v", *url, err)
	}

	if err := sc.Authorize(*username, *password); err != nil {
		log.Fatalf("Failed to authorize with server: %v", err)
	}

	if *cpuprofile != "" {
		time.Sleep(300 * time.Second)
	} else {
		wg.Wait() // blocks forever
	}
}
