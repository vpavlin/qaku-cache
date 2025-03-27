package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/waku-org/waku-go-bindings/waku"
	"github.com/waku-org/waku-go-bindings/waku/common"
)

const (
	envCodexApiUrl    = "CODEX_API_URL"
	envMaxDatasetSize = "QAKU_CACHE_MAX_SIZE"

	contentTopic   = "/0/qaku/1/persist/json"
	defaultMaxSize = 5 * 1024 * 1024
)

type QakuMessage struct {
	Type      string       `json:"type"`
	Payload   CacheRequest `json:"payload"`
	Timestamp int          `json:"timestamp"`
	Signature string       `json:"signature"`
	Signer    string       `json:"signer"`
}

type CacheRequest struct {
	CID   string `json:"cid"`
	Owner string `json:"owner"`
	Hash  string `json:"hash"`
}

type CodexManifest struct {
	DatasetSize int    `json:"datasetSize"`
	BlockSize   int    `json:"blockSize"`
	Protected   bool   `json:"protected"`
	TreeCid     string `json:"treeCid"`
	UploadedAt  string `json:"uploadedAt"`
}

type CodexDataContent struct {
	Cid      string        `json:"cid"`
	Manifest CodexManifest `json:"manifest"`
}

var maxDatasetSize = defaultMaxSize

var (
	snapSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "qaku_cache_successes",
		Help: "The total number successfully cached snapshot",
	})
	snapFailure = promauto.NewCounter(prometheus.CounterOpts{
		Name: "qaku_cache_failures",
		Help: "The total number failed attempts to cache a snapshot",
	})
	snapSizes = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "qaku_cache_sizes",
		Help:    "Histogram of sizes of cached snapshots",
		Buckets: []float64{100.0, 200.0, 500.0, 1000.0, 2000.0, 5000.0, 10000.0, 20000.0},
	})
)

func main() {
	go prom()

	//log.Panicln(os.Getenv(envMaxDatasetSize))
	maxSizeFromEnv, err := strconv.Atoi(os.Getenv(envMaxDatasetSize))
	if err != nil {
		log.Println(err)
	}

	if maxSizeFromEnv > 0 {
		maxDatasetSize = maxSizeFromEnv
	}

	nodeWakuConfig := common.WakuConfig{
		Relay:           true,
		LogLevel:        "DEBUG",
		Discv5Discovery: true,
		ClusterID:       42,
		Shards:          []uint16{getShardFromContentTopic("qaku", "1", 1)},
		Discv5UdpPort:   9000,
		TcpPort:         60000,
		Host:            "0.0.0.0",
		Staticnodes:     []string{"/dns4/node-01.do-ams3.waku.sandbox.status.im/tcp/30303/p2p/16Uiu2HAmNaeL4p3WEYzC9mgXBmBWSgWjPHRvatZTXnp8Jgv3iKsb"},
	}

	node, err := waku.NewWakuNode(&nodeWakuConfig, "node")
	if err != nil {
		fmt.Printf("Failed to create Waku node: %v\n", err)
		return
	}
	if err := node.Start(); err != nil {
		fmt.Printf("Failed to start Waku node: %v\n", err)
		return
	}
	defer node.Stop()
	time.Sleep(1 * time.Second)

	c := &Cache{}

	go func() {
		for envelope := range node.MsgChan {
			if envelope.Message().ContentTopic == contentTopic {
				c.OnNewEnvelope(envelope)
			}
		}
	}()

	server()
}

func server() {
	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:  []string{"http://localhost:3000", "https://qaku.app"},
		AllowMethods:  []string{"GET", "OPTIONS"},
		AllowHeaders:  []string{"Origin,DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range"},
		ExposeHeaders: []string{"Content-Length"},
	}))

	r.GET("/api/qaku/v1/info", func(c *gin.Context) {
		url := getCodexUrl()

		type DebugInfo struct {
			ID             string   `json:"id"`
			AnnouncedAddrs []string `json:"announceAddresses"`
		}

		var infoResp *http.Response
		infoResp, err := http.Get(fmt.Sprintf("%s/api/codex/v1/debug/info", url))
		if err != nil {
			log.Println("failed to fetch manifest", err)
			return
		}
		defer infoResp.Body.Close()

		body, err := io.ReadAll(infoResp.Body)
		if err != nil {
			log.Println("faild to read manifest data", err)
			return
		}

		info := &DebugInfo{}
		err = json.Unmarshal(body, info)
		if err != nil {
			log.Println("failed to unmarshal manifest: ", err)
			return
		}

		c.JSON(200, gin.H{"peerId": info.ID, "addr": info.AnnouncedAddrs[0]})
	})

	r.GET("/api/qaku/v1/snapshot/:cid", func(c *gin.Context) {
		url := getCodexUrl()
		cid := c.Param("cid")
		log.Println(c.Params)

		if cid == "" {
			c.Error(fmt.Errorf("empty CID param"))
			c.String(500, "empty CID param")
			return
		}

		var cidResp *http.Response
		cidResp, err := http.Get(fmt.Sprintf("%s/api/codex/v1/data/%s", url, cid))
		if err != nil {
			c.Error(fmt.Errorf("failed to fetch manifest: %s", err))
			return
		}
		defer cidResp.Body.Close()

		io.Copy(c.Writer, cidResp.Body)
		c.Status(cidResp.StatusCode)

	})

	log.Fatal(r.Run("0.0.0.0:8080"))
}

func prom() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":8003", nil)
}

func getCodexUrl() string {
	url := os.Getenv(envCodexApiUrl)
	if url == "" {
		url = "http://codex:8080"
	}

	return url
}

type Cache struct {
}

func (c *Cache) OnNewEnvelope(envelope common.Envelope) error {
	log.Println(envelope)
	var err error
	defer func() {
		if err != nil {
			snapFailure.Inc()
		}
	}()
	log.Println(string(envelope.Message().Payload))
	cr := &QakuMessage{}
	err = json.Unmarshal(envelope.Message().Payload, cr)
	if err != nil {
		log.Println("failed to unmarshal: ", err)
		return err
	}

	url := getCodexUrl()

	var manifestResp *http.Response
	manifestResp, err = http.Get(fmt.Sprintf("%s/api/codex/v1/data/%s/network/manifest", url, cr.Payload.CID))
	if err != nil {
		log.Println("failed to fetch manifest", err)
		return err
	}
	defer manifestResp.Body.Close()

	if manifestResp.StatusCode != 200 {
		err = fmt.Errorf("failed to fetch manifest")
		log.Println("failed to fetch manifest", manifestResp.Status)
		return err
	}

	body, err := io.ReadAll(manifestResp.Body)
	if err != nil {
		log.Println("faild to read manifest data", err)
		return err
	}

	cdc := &CodexDataContent{}
	err = json.Unmarshal(body, cdc)
	if err != nil {
		log.Println("failed to unmarshal manifest: ", err)
		return err
	}

	if cdc.Manifest.DatasetSize > maxDatasetSize {
		log.Printf("dataset too big %d > %d", cdc.Manifest.DatasetSize, maxDatasetSize)
		return err
	}

	snapSizes.Observe(float64(cdc.Manifest.DatasetSize) / 1024)

	var resp *http.Response
	resp, err = http.Post(fmt.Sprintf("%s/api/codex/v1/data/%s/network", url, cr.Payload.CID), "", nil)
	if err != nil {
		log.Println("failed to send request: ", err)
		return err
	}

	if resp.StatusCode != 200 {
		err = fmt.Errorf("request to Codex failed")
		log.Println("request to Codex failed: ", resp.Status)
		return err
	}

	snapSuccess.Inc()

	return nil
}

func getShardFromContentTopic(appName string, appVersion string, shardCount int) uint16 {
	bytes := []byte(appName)
	bytes = append(bytes, []byte(appVersion)...)

	hash := sha256.Sum256(bytes)
	//We only use the last 64 bits of the hash as having more shards is unlikely.
	hashValue := binary.BigEndian.Uint64(hash[24:])

	shard := hashValue % uint64(shardCount)

	return uint16(shard)
}
