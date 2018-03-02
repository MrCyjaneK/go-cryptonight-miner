package main

import (
	"io/ioutil"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/alecthomas/kingpin"
	stratum "github.com/gurupras/go-stratum-client"
	gpuminer "github.com/gurupras/go-stratum-client/gpu-miner"
	amdgpu "github.com/gurupras/go-stratum-client/gpu-miner/amd"
	"github.com/gurupras/go-stratum-client/miner"
	log "github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"
)

var (
	app        = kingpin.New("cpuminer", "CPU Cryptonight miner")
	config     = app.Flag("config-file", "YAML config file").Short('c').Required().String()
	verbose    = app.Flag("verbose", "Enable verbose log messages").Short('v').Bool()
	cpuprofile = app.Flag("cpuprofile", "Run CPU profiler").String()
)

// Ripoff from minerconfig

// Config structure representing config JSON file
// Add any relevant fields here
type Config struct {
	Algorithm      string      `json:"algo" yaml:"algo"`
	Background     bool        `json:"background" yaml:"background"`
	Colors         bool        `json:"colors" yaml:"colors"`
	DonateLevel    float64     `json:"donate-level" yaml:"donate-level"`
	LogFile        *string     `json:"log-file" yaml:"log-file"`
	PrintTime      int         `json:"print-time" yaml:"print-time"`
	Retries        int         `json:"retries" yaml:"retries"`
	RetryPause     int         `json:"retry-pause" yaml:"retry-pause"`
	Syslog         bool        `json:"syslog" yaml:"syslog"`
	OpenCLPlatform int         `json:"opencl-platform" yaml:"opencl-platform"`
	Threads        []GPUThread `json:"threads" yaml:"threads"`
	Pools          []Pool      `json:"pools" yaml:"pools"`
	// Arguments to support miners like cpuminer-multi
	Url   string `json:"url" yaml:"url"`
	User  string `json:"user" yaml:"user"`
	Pass  string `json:"pass" yaml:"pass"`
	Proxy string `json:"proxy" yaml:"proxy"`
}

// GPUThread structure representing a GPU thread
type GPUThread struct {
	Index       int  `json:"index" yaml:"index"`
	Intensity   int  `json:"intensity" yaml:"intensity"`
	WorkSize    int  `json:"worksize" yaml:"worksize"`
	AffineToCPU bool `json:"affine_to_cpu" yaml:"affine_to_cpu"`
}

// Pool structure representing a pool
type Pool struct {
	Url        string  `json:"url" yaml:"url"`
	User       string  `json:"user" yaml:"user"`
	Pass       string  `json:"pass" yaml:"pass"`
	Keepalive  bool    `json:"keepalive" yaml:"keepalive"`
	Nicehash   bool    `json:"nicehash" yaml:"nicehash"`
	Coin       *string `json:"coin" yaml:"coin"`
	PoolName   *string `json:"pool_name" yaml:"pool_name"`
	WalletName *string `json:"wallet_name" yaml:"wallet_name"`
	Label      *string `json:"label" yaml:"label"`
}

func main() {
	kingpin.MustParse(app.Parse(os.Args[1:]))

	if *verbose {
		log.SetLevel(log.DebugLevel)
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

	// Parse config file and extract necessary fields
	configData, err := ioutil.ReadFile(*config)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	var config Config
	if err := yaml.Unmarshal(configData, &config); err != nil {
		log.Fatalf("Failed to parse yaml into valid config: %v", err)
	}

	sc := stratum.New()

	hashrateChan := make(chan *miner.HashRate)
	go func() {
		duration := 10 * time.Second
		totalHashes := uint32(0)

		startTime := time.Now()
		for hr := range hashrateChan {
			now := time.Now()
			if now.Sub(startTime) < duration {
				totalHashes += hr.Hashes
			} else {
				log.Infof("Speed: %dH/s", uint32(float64(totalHashes)/(now.Sub(startTime).Seconds())))
				totalHashes = 0
				startTime = time.Now()
			}
		}
	}()

	numMiners := len(config.Threads)
	miners := make([]miner.Interface, numMiners)
	gpuContexts := make([]*amdgpu.GPUContext, numMiners)

	for i := 0; i < numMiners; i++ {
		threadInfo := config.Threads[i]
		miner := gpuminer.NewGPUMiner(sc, threadInfo.Index, threadInfo.Intensity, threadInfo.WorkSize)
		miner.RegisterHashrateListener(hashrateChan)
		gpuContexts[i] = miner.Context
		miners[i] = miner
	}

	if err := amdgpu.InitOpenCL(gpuContexts, numMiners, config.OpenCLPlatform); err != nil {
		log.Fatalf("Failed to initialize OpenCL: %v", err)
	}

	go gpuminer.RunHashChecker()

	wg := sync.WaitGroup{}
	wg.Add(1)
	for i := 0; i < numMiners; i++ {
		go miners[i].Run()
	}

	// responseChan := make(chan *stratum.Response)
	//
	// sc.RegisterResponseListener(responseChan)

	pool := config.Pools[0]
	if err := sc.Connect(pool.Url); err != nil {
		log.Fatalf("Failed to connect to url :%v  - %v", pool.Url, err)
	}

	if err := sc.Authorize(pool.User, pool.Pass); err != nil {
		log.Fatalf("Failed to authorize with server: %v", err)
	}

	if *cpuprofile != "" {
		time.Sleep(300 * time.Second)
	} else {
		wg.Wait() // blocks forever
	}
}
