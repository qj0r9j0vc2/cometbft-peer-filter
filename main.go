package main

import (
	"encoding/json"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	Timeout    = 30
	targetHost = "localhost:26657" // formerly InitialHost
	TopPeers   = 5                 // top N peers to display
)

// Status represents transfer status (embedded in ConnectionStatus)
type Status struct {
	Start    time.Time // Transfer start time
	Bytes    string
	Samples  string
	InstRate string
	CurRate  string
	AvgRate  string
	PeakRate string
	BytesRem string
	Duration string
	Idle     string
	TimeRem  string
	Progress Percent
	Active   bool
}

type Percent uint32

// CometBFTNetInfoResult and related types (for unmarshaling net_info)
type CometBFTNetInfoResult struct {
	Result  ResultNetInfo `json:"result"`
	ID      any           `json:"id"`
	Jsonrpc string        `json:"jsonrpc"`
}

type ResultNetInfo struct {
	Listening bool     `json:"listening"`
	Listeners []string `json:"listeners"`
	NPeers    string   `json:"n_peers"`
	Peers     []Peer   `json:"peers"`
}

type Peer struct {
	NodeInfo         DefaultNodeInfo  `json:"node_info"`
	IsOutbound       bool             `json:"is_outbound"`
	ConnectionStatus ConnectionStatus `json:"connection_status"`
	RemoteIP         string           `json:"remote_ip"`
}

type ConnectionStatus struct {
	Duration    string
	SendMonitor Status
	RecvMonitor Status
	Channels    []ChannelStatus
}

type ChannelStatus struct {
	ID                byte
	SendQueueCapacity string
	SendQueueSize     string
	Priority          string
	RecentlySent      string
}

type DefaultNodeInfo struct {
	ProtocolVersion ProtocolVersion      `json:"protocol_version"`
	DefaultNodeID   string               `json:"id"`
	ListenAddr      string               `json:"listen_addr"`
	Network         string               `json:"network"`
	Version         string               `json:"version"`
	Channels        HexBytes             `json:"channels"`
	Moniker         string               `json:"moniker"`
	Other           DefaultNodeInfoOther `json:"other"`
}

type ProtocolVersion struct {
	P2P   string `json:"p2p"`
	Block string `json:"block"`
	App   string `json:"app"`
}

type HexBytes string

type DefaultNodeInfoOther struct {
	TxIndex    string `json:"tx_index"`
	RPCAddress string `json:"rpc_address"`
}

func main() {
	log.SetLevel(log.InfoLevel)

	client := &http.Client{
		Timeout: Timeout * time.Second,
	}

	// Fetch the peer info from targetHost's /net_info endpoint.
	resp, err := client.Get(addPrefix(fmt.Sprintf("%s/net_info", targetHost)))
	if err != nil {
		log.Fatalf("Error fetching net_info from target host %s: %v", targetHost, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	var netInfoRes CometBFTNetInfoResult
	if err = json.Unmarshal(body, &netInfoRes); err != nil {
		log.Fatalf("Error unmarshaling JSON: %v", err)
	}

	peers := netInfoRes.Result.Peers

	// Create a slice to hold peers with their total transferred bytes.
	type peerWithBytes struct {
		peer       Peer
		totalBytes int64
	}
	var peersWithBytes []peerWithBytes

	for _, p := range peers {

		// Parse the "Bytes" fields from both SendMonitor and RecvMonitor.
		sendBytes, _ := parseBytes(p.ConnectionStatus.SendMonitor.Bytes)
		recvBytes, _ := parseBytes(p.ConnectionStatus.RecvMonitor.Bytes)
		total := sendBytes + recvBytes

		peersWithBytes = append(peersWithBytes, peerWithBytes{
			peer:       p,
			totalBytes: total,
		})
	}

	// Sort the peers by total bytes transferred in descending order.
	sort.Slice(peersWithBytes, func(i, j int) bool {
		return peersWithBytes[i].totalBytes > peersWithBytes[j].totalBytes
	})

	// Select the top N peers.
	topCount := TopPeers
	if len(peersWithBytes) < TopPeers {
		topCount = len(peersWithBytes)
	}
	topPeers := peersWithBytes[:topCount]

	var resultFile string
	log.Infof("Top %d peers by bytes transferred:", topCount)
	for idx, p := range topPeers {
		log.Infof("Peer: %s, TotalBytes: %d, Moniker: %s, Network: %s",
			p.peer.RemoteIP,
			p.totalBytes,
			p.peer.NodeInfo.Moniker,
			p.peer.NodeInfo.Network,
		)
		ListenAddr := p.peer.NodeInfo.ListenAddr
		if strings.Contains(ListenAddr, "0.0.0.0") {
			ListenAddr = strings.Replace(ListenAddr, "0.0.0.0", p.peer.RemoteIP, -1)
		}
		resultFile = fmt.Sprintf("%s%s@%s", resultFile, p.peer.NodeInfo.DefaultNodeID, ListenAddr)
		if idx < len(topPeers)-1 {
			resultFile = fmt.Sprintf("%s,", resultFile)
		}
	}

	err = os.WriteFile("peers.txt", []byte(resultFile), 0644)
	if err != nil {
		log.Fatalf("Error writing result file: %v", err)
	}
}

// parseBytes converts a string (assumed to represent a number) to int64.
// On error, it returns 0.
func parseBytes(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// addPrefix ensures the URL has an "http://" prefix.
func addPrefix(host string) string {
	if strings.HasPrefix(host, "http") {
		return host
	}
	return fmt.Sprintf("http://%s", host)
}
