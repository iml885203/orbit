package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/iml885203/orbit/config"
)

// SetupSidecars configures sidecars that need initial setup after starting.
func SetupSidecars(parentName string, cfg *config.Container) {
	for _, sc := range cfg.Sidecars {
		if strings.Contains(sc.Image, "redisinsight") {
			setupRedisInsight(parentName, cfg, &sc)
		}
	}
}

func setupRedisInsight(parentName string, cfg *config.Container, sc *config.Sidecar) {
	pd, ok := sc.Ports["ui"]
	if !ok {
		return
	}

	baseURL := fmt.Sprintf("http://localhost:%d", pd.Host)
	dbsURL := baseURL + "/api/databases"
	client := &http.Client{Timeout: 2 * time.Second}

	// Wait for RedisInsight to be ready and check if already configured
	var dbs []map[string]any
	for range 30 { // ~60s max wait
		resp, err := client.Get(dbsURL)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		err = json.NewDecoder(resp.Body).Decode(&dbs)
		_ = resp.Body.Close()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}

	if len(dbs) > 0 {
		return // already configured
	}

	redisHost := ContainerName(os.Getenv("ORBIT_NAMESPACE"), parentName)
	redisPort := 6379
	if redisPd, ok := cfg.Ports["redis"]; ok {
		redisPort = redisPd.Target
	}

	body, _ := json.Marshal(map[string]any{
		"name": redisHost,
		"host": redisHost,
		"port": redisPort,
	})

	addResp, err := client.Post(dbsURL, "application/json", bytes.NewReader(body))
	if err != nil {
		slog.Error("redis-insight setup failed", "component", "sidecar", "err", err)
		return
	}
	_ = addResp.Body.Close()
	slog.Info("redis-insight auto-configured", "component", "sidecar", "host", redisHost, "port", redisPort)
}
