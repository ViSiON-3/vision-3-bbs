// Command v3net-bootstrap creates and publishes an initial NAL for a V3Net
// network. It loads the hub's keystore, builds a NAL with the specified areas,
// signs it, and POSTs it to the hub.
//
// Usage:
//
//	go run ./cmd/v3net-bootstrap -keystore data/v3net.key -hub http://localhost:8765 -network felonynet -area fel.general:FelonyNet General
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/keystore"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/nal"
	"github.com/ViSiON-3/vision-3-bbs/internal/v3net/protocol"
)

func main() {
	keystorePath := flag.String("keystore", "", "path to v3net.key")
	hubURL := flag.String("hub", "http://localhost:8765", "hub URL")
	network := flag.String("network", "", "network name (e.g. felonynet)")
	areas := flag.String("areas", "", "comma-separated tag:Name pairs (e.g. fel.general:FelonyNet General)")
	flag.Parse()

	if *keystorePath == "" || *network == "" || *areas == "" {
		flag.Usage()
		os.Exit(1)
	}

	ks, err := keystore.Load(*keystorePath)
	if err != nil {
		log.Fatalf("load keystore: %v", err)
	}
	fmt.Printf("Node ID: %s\n", ks.NodeID())

	// Parse area definitions.
	var areaList []protocol.Area
	for _, spec := range strings.Split(*areas, ",") {
		parts := strings.SplitN(spec, ":", 2)
		if len(parts) != 2 {
			log.Fatalf("invalid area spec %q: expected tag:Name", spec)
		}
		tag, name := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		areaList = append(areaList, protocol.Area{
			Tag:              tag,
			Name:             name,
			Language:         "en",
			ManagerNodeID:    ks.NodeID(),
			ManagerPubKeyB64: ks.PubKeyBase64(),
			Access:           protocol.AreaAccess{Mode: protocol.AccessModeOpen},
			Policy: protocol.AreaPolicy{
				MaxBodyBytes: 64000,
				AllowANSI:    true,
			},
		})
	}

	n := &protocol.NAL{
		V3NetNAL: "1.0",
		Network:  *network,
		Areas:    areaList,
	}

	if err := nal.Sign(n, ks); err != nil {
		log.Fatalf("sign NAL: %v", err)
	}
	fmt.Printf("Signed NAL: network=%s, areas=%d, updated=%s\n", n.Network, len(n.Areas), n.Updated)

	// POST to hub.
	body, err := json.Marshal(n)
	if err != nil {
		log.Fatalf("marshal NAL: %v", err)
	}

	path := fmt.Sprintf("/v3net/v1/%s/nal", *network)
	url := *hubURL + path

	bodyHash := sha256.Sum256(body)
	bodySHA := hex.EncodeToString(bodyHash[:])
	dateUTC := time.Now().UTC().Format(http.TimeFormat)

	sig, err := ks.Sign("POST", path, dateUTC, bodySHA)
	if err != nil {
		log.Fatalf("sign request: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		log.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", dateUTC)
	req.Header.Set("X-V3Net-Node-ID", ks.NodeID())
	req.Header.Set("X-V3Net-Signature", sig)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("POST NAL: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response: %d %s\n", resp.StatusCode, string(respBody))

	if resp.StatusCode != http.StatusOK {
		os.Exit(1)
	}

	fmt.Println("NAL published successfully!")
}
