package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/castai-labs/woop-pod-resource-guardrail/internal/kube"
	"github.com/castai-labs/woop-pod-resource-guardrail/internal/webhook"
)

func main() {
	reader, err := kube.NewInClusterBudgetReader()
	if err != nil {
		log.Fatalf("initialize Kubernetes client: %v", err)
	}
	port := env("PORT", "8443")
	certFile := env("TLS_CERT_FILE", "/tls/tls.crt")
	keyFile := env("TLS_KEY_FILE", "/tls/tls.key")

	mux := http.NewServeMux()
	mux.Handle("/validate", webhook.NewHandler(reader))
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	log.Printf("WOOP pod resource guardrail listening on %s", server.Addr)
	if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
		log.Fatalf("serve webhook: %v", err)
	}
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
