package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type BackendTarget struct {
	URL     *url.URL
	Healthy bool
}

type RouteGroup struct {
	Prefix  string
	Targets []*BackendTarget
	counter uint64
	mu      sync.RWMutex
}

type ConfigRoute struct {
	Prefix  string   `json:"prefix"`
	Targets []string `json:"targets"`
}

var (
	routeGroups []*RouteGroup
	proxyMap    sync.Map
)

func (rg *RouteGroup) next() *BackendTarget {
	rg.mu.RLock()
	defer rg.mu.RUnlock()

	var healthyTargets []*BackendTarget
	for _, t := range rg.Targets {
		if t.Healthy {
			healthyTargets = append(healthyTargets, t)
		}
	}

	if len(healthyTargets) == 0 {
		return nil
	}

	idx := atomic.AddUint64(&rg.counter, 1) % uint64(len(healthyTargets))
	return healthyTargets[idx]
}

func loadRoutes(filePath string) ([]*RouteGroup, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var configs []ConfigRoute
	if err := json.Unmarshal(file, &configs); err != nil {
		return nil, err
	}

	var groups []*RouteGroup
	for _, cfg := range configs {
		group := &RouteGroup{Prefix: cfg.Prefix}
		for _, targetStr := range cfg.Targets {
			parsedURL, err := url.Parse(targetStr)
			if err != nil {
				log.Printf("[API Gateway] URL inválida omitida %s: %v", targetStr, err)
				continue
			}
			group.Targets = append(group.Targets, &BackendTarget{
				URL:     parsedURL,
				Healthy: true,
			})
		}
		groups = append(groups, group)
	}
	return groups, nil
}

func startHealthChecks(interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			client := http.Client{Timeout: 2 * time.Second}
			for _, group := range routeGroups {
				group.mu.Lock()
				for _, target := range group.Targets {
					heartbeatURL := fmt.Sprintf("%s://%s/heartbeat", target.URL.Scheme, target.URL.Host)
					resp, err := client.Get(heartbeatURL)

					isHealthy := err == nil && resp.StatusCode == http.StatusOK
					if resp != nil {
						resp.Body.Close()
					}

					if target.Healthy != isHealthy {
						target.Healthy = isHealthy
						log.Printf("[HealthCheck] Instancia %s cambio estado a Healthy=%t", target.URL.Host, isHealthy)
					}
				}
				group.mu.Unlock()
			}
		}
	}()
}

func getProxy(targetURL *url.URL) *httputil.ReverseProxy {
	if proxy, ok := proxyMap.Load(targetURL.String()); ok {
		return proxy.(*httputil.ReverseProxy)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxyMap.Store(targetURL.String(), proxy)
	return proxy
}

func main() {
	var err error
	routesFile := getEnv("ROUTES_FILE", "routes.json")

	routeGroups, err = loadRoutes(routesFile)
	if err != nil {
		log.Fatalf("[API Gateway] Error cargando %s: %v", routesFile, err)
	}

	startHealthChecks(5 * time.Second)

	maxConcurrentRequests := 100
	semaphore := make(chan struct{}, maxConcurrentRequests)

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var response []map[string]interface{}

		for _, group := range routeGroups {
			group.mu.RLock()
			for _, target := range group.Targets {
				response = append(response, map[string]interface{}{
					"prefix":  group.Prefix,
					"target":  target.URL.String(),
					"healthy": target.Healthy,
				})
			}
			group.mu.RUnlock()
		}
		json.NewEncoder(w).Encode(response)
	})

	http.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		semaphore <- struct{}{}
		defer func() { <-semaphore }()

		var selectedGroup *RouteGroup
		for _, group := range routeGroups {
			if strings.HasPrefix(r.URL.Path, group.Prefix) {
				selectedGroup = group
				break
			}
		}

		if selectedGroup == nil {
			http.Error(w, "Ruta no encontrada en el Gateway", http.StatusNotFound)
			return
		}

		target := selectedGroup.next()
		if target == nil {
			http.Error(w, "503 Service Unavailable: Sin backends saludables", http.StatusServiceUnavailable)
			return
		}

		log.Printf("[API Gateway] Round-Robin -> %s %s dirigida a %s", r.Method, r.URL.Path, target.URL.Host)

		proxy := getProxy(target.URL)
		proxy.ServeHTTP(w, r)
	})

	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	puerto := getEnv("PORT", "8001")
	log.Printf("[API Gateway] Corriendo en http://localhost:%s", puerto)

	if err := http.ListenAndServe(":"+puerto, nil); err != nil {
		log.Fatalf("[API Gateway] Error iniciando el servidor: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}