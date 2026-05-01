package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/client"
)

func TestStatsHandler(t *testing.T) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker não disponível, pulando teste de integração")
		return
	}
	defer docker.Close()

	collector := NewCollector(docker)
	handler := StatsHandler(collector)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("handler returned wrong content type: got %v want %v", contentType, "application/json")
	}
}

func TestComputeCPUPercent(t *testing.T) {
	tests := []struct {
		name       string
		systemNow  uint64
		systemPrev uint64
		cpuNow     uint64
		cpuPrev    uint64
		online     uint32
		expected   float64
	}{
		{"zero delta", 100, 100, 50, 50, 1, 0},
		{"normal usage", 200, 100, 110, 100, 1, 10},
		{"multi core", 200, 100, 110, 100, 4, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCPUPercent(tt.systemNow, tt.systemPrev, tt.cpuNow, tt.cpuPrev, tt.online, tt.online, nil)
			if got != tt.expected {
				t.Errorf("computeCPUPercent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestClampPercent(t *testing.T) {
	if clampPercent(150) != 150 {
		t.Errorf("clampPercent(150) = %v, want 150", clampPercent(150))
	}
	if clampPercent(-10) != 0 {
		t.Errorf("clampPercent(-10) = %v, want 0", clampPercent(-10))
	}
	if clampPercent(200000) != 100*1024 {
		t.Errorf("clampPercent(200000) = %v, want %v", clampPercent(200000), 100*1024)
	}
}

func TestLogsStreamerHandler_Validation(t *testing.T) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker não disponível, pulando teste de integração")
		return
	}
	defer docker.Close()

	streamer := NewLogsStreamer(docker)
	handler := streamer.Handler()

	// Teste sem parâmetro 'container'
	req := httptest.NewRequest("GET", "/api/ws/logs", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code for missing param: got %v want %v", status, http.StatusBadRequest)
	}

	// Teste com container inexistente
	req = httptest.NewRequest("GET", "/api/ws/logs?container=non-existent-container-id", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("handler returned wrong status code for non-existent container: got %v want %v", status, http.StatusNotFound)
	}
}
