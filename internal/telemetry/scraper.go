package telemetry

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-engine/internal/api"
)

// Scraper polls each node's DCGM exporter (/metrics on :9400) for GPU health
// signals: framebuffer usage, utilization, temperature, Xid errors. The
// engine aggregates per-node metrics into Node.GPU and derives conditions
// from the Xid table (Degraded / Draining triggers).
type Scraper struct {
	Client  *http.Client
	Timeout time.Duration
}

func NewScraper() *Scraper {
	return &Scraper{Client: &http.Client{Timeout: 5 * time.Second}, Timeout: 5 * time.Second}
}

// ScrapeNode fetches the node's DCGM endpoint and returns the aggregated
// GPUStatus, or nil when the exporter is unreachable (node stays as-is).
func (s *Scraper) ScrapeNode(ctx context.Context, node api.Node) (*api.GPUStatus, error) {
	if node.IP == "" {
		return nil, fmt.Errorf("node %s has no IP", node.ID)
	}
	url := fmt.Sprintf("http://%s:9400/metrics", node.IP)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dcgm %s: %s", node.ID, resp.Status)
	}
	return parseDCGM(resp.Body, node.GPU)
}

// parseDCGM reads the Prometheus text exposition format and aggregates the
// per-GPU DCGM metrics into a single node-level GPUStatus.
func parseDCGM(r io.Reader, prev *api.GPUStatus) (*api.GPUStatus, error) {
	var (
		fbUsedMiB, fbTotalMiB          float64
		utilMax, tempMax               int
		xids                           []int
		model                          string
		seen                           bool
	)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, labels, val, ok := splitMetric(line)
		if !ok {
			continue
		}
		seen = true
		switch name {
		case "DCGM_FI_DEV_FB_USED":
			fbUsedMiB += val
		case "DCGM_FI_DEV_FB_TOTAL":
			fbTotalMiB += val
		case "DCGM_FI_DEV_GPU_UTIL":
			if int(val) > utilMax {
				utilMax = int(val)
			}
		case "DCGM_FI_DEV_GPU_TEMP":
			if int(val) > tempMax {
				tempMax = int(val)
			}
		case "DCGM_FI_DEV_XID_ERRORS":
			if val > 0 {
				xids = append(xids, int(val))
			}
		case "DCGM_FI_DEV_MEM_COPY_UTIL":
			// memory utilization is a secondary signal; VRAM is authoritative
		}
		if m := labels["modelName"]; m != "" && model == "" {
			model = m
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !seen {
		return nil, fmt.Errorf("no dcgm metrics in scrape")
	}
	if fbTotalMiB == 0 && prev != nil && prev.VRAMTotalGB > 0 {
		fbTotalMiB = prev.VRAMTotalGB * 1024
	}
	if model == "" && prev != nil {
		model = prev.Model
	}
	return &api.GPUStatus{
		Model:       model,
		VRAMUsedGB:  fbUsedMiB / 1024,
		VRAMTotalGB: fbTotalMiB / 1024,
		Utilization: utilMax,
		TempC:       tempMax,
		XidErrors:   xids,
	}, nil
}

// splitMetric parses `name{label="v",...} value` into parts.
func splitMetric(line string) (string, map[string]string, float64, bool) {
	lidx := strings.LastIndexByte(line, ' ')
	if lidx < 0 {
		return "", nil, 0, false
	}
	val, err := strconv.ParseFloat(line[lidx+1:], 64)
	if err != nil {
		return "", nil, 0, false
	}
	head := line[:lidx]
	name, labels := head, map[string]string{}
	if i := strings.IndexByte(head, '{'); i >= 0 && strings.HasSuffix(head, "}") {
		name = head[:i]
		for _, kv := range strings.Split(head[i+1:len(head)-1], ",") {
			k, v, found := strings.Cut(kv, "=")
			if found {
				labels[k] = strings.Trim(v, `"`)
			}
		}
	}
	return name, labels, val, true
}
