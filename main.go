package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/waku-org/go-waku/waku/v2/node"
	"github.com/waku-org/go-waku/waku/v2/protocol"
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

	hostAddr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:0")

	nodes := []string{
		"enr:-QEkuEBIkb8q8_mrorHndoXH9t5N6ZfD-jehQCrYeoJDPHqT0l0wyaONa2-piRQsi3oVKAzDShDVeoQhy0uwN1xbZfPZAYJpZIJ2NIJpcIQiQlleim11bHRpYWRkcnO4bgA0Ni9ub2RlLTAxLmdjLXVzLWNlbnRyYWwxLWEud2FrdS5zYW5kYm94LnN0YXR1cy5pbQZ2XwA2Ni9ub2RlLTAxLmdjLXVzLWNlbnRyYWwxLWEud2FrdS5zYW5kYm94LnN0YXR1cy5pbQYfQN4DgnJzkwABCAAAAAEAAgADAAQABQAGAAeJc2VjcDI1NmsxoQKnGt-GSgqPSf3IAPM7bFgTlpczpMZZLF3geeoNNsxzSoN0Y3CCdl-DdWRwgiMohXdha3UyDw",
		"enr:-QESuEB4Dchgjn7gfAvwB00CxTA-nGiyk-aALI-H4dYSZD3rUk7bZHmP8d2U6xDiQ2vZffpo45Jp7zKNdnwDUx6g4o6XAYJpZIJ2NIJpcIRA4VDAim11bHRpYWRkcnO4XAArNiZub2RlLTAxLmRvLWFtczMud2FrdS5zYW5kYm94LnN0YXR1cy5pbQZ2XwAtNiZub2RlLTAxLmRvLWFtczMud2FrdS5zYW5kYm94LnN0YXR1cy5pbQYfQN4DgnJzkwABCAAAAAEAAgADAAQABQAGAAeJc2VjcDI1NmsxoQOvD3S3jUNICsrOILlmhENiWAMmMVlAl6-Q8wRB7hidY4N0Y3CCdl-DdWRwgiMohXdha3UyDw",
		"enr:-QEkuEBfEzJm_kigJ2HoSS_RBFJYhKHocGdkhhBr6jSUAWjLdFPp6Pj1l4yiTQp7TGHyu1kC6FyaU573VN8klLsEm-XuAYJpZIJ2NIJpcIQI2SVcim11bHRpYWRkcnO4bgA0Ni9ub2RlLTAxLmFjLWNuLWhvbmdrb25nLWMud2FrdS5zYW5kYm94LnN0YXR1cy5pbQZ2XwA2Ni9ub2RlLTAxLmFjLWNuLWhvbmdrb25nLWMud2FrdS5zYW5kYm94LnN0YXR1cy5pbQYfQN4DgnJzkwABCAAAAAEAAgADAAQABQAGAAeJc2VjcDI1NmsxoQOwsS69tgD7u1K50r5-qG5hweuTwa0W26aYPnvivpNlrYN0Y3CCdl-DdWRwgiMohXdha3UyDw",
		"enr:-QEMuEDbayK340kH24XzK5FPIYNzWNYuH01NASNIb1skZfe_6l4_JSsG-vZ0LgN4Cgzf455BaP5zrxMQADHL5OQpbW6OAYJpZIJ2NIJpcISygI2rim11bHRpYWRkcnO4VgAoNiNub2RlLTAxLmRvLWFtczMud2FrdS50ZXN0LnN0YXR1cy5pbQZ2XwAqNiNub2RlLTAxLmRvLWFtczMud2FrdS50ZXN0LnN0YXR1cy5pbQYfQN4DgnJzkwABCAAAAAEAAgADAAQABQAGAAeJc2VjcDI1NmsxoQJATXRSRSUyTw_QLB6H_U3oziVQgNRgrXpK7wp2AMyNxYN0Y3CCdl-DdWRwgiMohXdha3UyDw",
		"enr:-QEeuEBO08GSjWDOV13HTf6L7iFoPQhv4S0-_Bd7Of3lFCBNBmpB9j6pGLedkX88KAXm6BFCS4ViQ_rLeDQuzj9Q6fs9AYJpZIJ2NIJpcIQiEAFDim11bHRpYWRkcnO4aAAxNixub2RlLTAxLmdjLXVzLWNlbnRyYWwxLWEud2FrdS50ZXN0LnN0YXR1cy5pbQZ2XwAzNixub2RlLTAxLmdjLXVzLWNlbnRyYWwxLWEud2FrdS50ZXN0LnN0YXR1cy5pbQYfQN4DgnJzkwABCAAAAAEAAgADAAQABQAGAAeJc2VjcDI1NmsxoQMIJwesBVgUiBCi8yiXGx7RWylBQkYm1U9dvEy-neLG2YN0Y3CCdl-DdWRwgiMohXdha3UyDw",
		"enr:-QEeuECvvBe6kIzHgMv_mD1YWQ3yfOfid2MO9a_A6ZZmS7E0FmAfntz2ZixAnPXvLWDJ81ARp4oV9UM4WXyc5D5USdEPAYJpZIJ2NIJpcIQI2ttrim11bHRpYWRkcnO4aAAxNixub2RlLTAxLmFjLWNuLWhvbmdrb25nLWMud2FrdS50ZXN0LnN0YXR1cy5pbQZ2XwAzNixub2RlLTAxLmFjLWNuLWhvbmdrb25nLWMud2FrdS50ZXN0LnN0YXR1cy5pbQYfQN4DgnJzkwABCAAAAAEAAgADAAQABQAGAAeJc2VjcDI1NmsxoQJIN4qwz3v4r2Q8Bv8zZD0eqBcKw6bdLvdkV7-JLjqIj4N0Y3CCdl-DdWRwgiMohXdha3UyDw",
	}

	enodes := []*enode.Node{}
	for _, n := range nodes {
		e, err := enode.Parse(enode.ValidSchemes, n)
		if err != nil {
			log.Fatal(err)
		}

		enodes = append(enodes, e)
	}

	node, err := node.New(
		node.WithHostAddress(hostAddr),
		node.WithWakuFilterLightNode(),
		node.WithDiscoveryV5(uint(9000), enodes, true),
		//node.WithLogLevel(zap.DebugLevel),
		node.WithClusterID(uint16(1)),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = node.Start(ctx)
	if err != nil {
		log.Fatal(err)
	}

	err = node.DiscV5().Start(ctx)
	if err != nil {
		log.Fatal(err)
	}

	time.Sleep(5 * time.Second)

	cf := protocol.ContentFilter{
		ContentTopics: protocol.NewContentTopicSet(contentTopic),
	}

	subs, err := node.FilterLightnode().Subscribe(ctx, cf)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Starting main loop")

	for envelope := range subs[0].C {
		log.Println("Got a message!!!")
		go cache(envelope)
	}
}

func prom() {
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(":8003", nil)
}

func cache(envelope *protocol.Envelope) {
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
		return
	}

	url := os.Getenv(envCodexApiUrl)
	if url == "" {
		url = "http://codex:8080"
	}

	var manifestResp *http.Response
	manifestResp, err = http.Get(fmt.Sprintf("%s/api/codex/v1/data/%s/network/manifest", url, cr.Payload.CID))
	if err != nil {
		log.Println("failed to fetch manifest", err)
		return
	}
	defer manifestResp.Body.Close()

	if manifestResp.StatusCode != 200 {
		log.Println("failed to fetch manifest", manifestResp.Status)
		return
	}

	body, err := ioutil.ReadAll(manifestResp.Body)
	if err != nil {
		log.Println("faild to read manifest data", err)
		return
	}

	log.Println(string(body))

	cdc := &CodexDataContent{}
	err = json.Unmarshal(body, cdc)
	if err != nil {
		log.Println("failed to unmarshal manifest: ", err)
		return
	}

	if cdc.Manifest.DatasetSize > maxDatasetSize {
		log.Printf("dataset too big %d > %d", cdc.Manifest.DatasetSize, maxDatasetSize)
		return
	}

	var resp *http.Response
	resp, err = http.Post(fmt.Sprintf("%s/api/codex/v1/data/%s/network", url, cr.Payload.CID), "", nil)
	if err != nil {
		log.Println("failed to send request: ", err)
		return
	}

	if resp.StatusCode != 200 {
		log.Println("request to Codex failed: ", resp.Status)
		return
	}

	snapSuccess.Inc()
}