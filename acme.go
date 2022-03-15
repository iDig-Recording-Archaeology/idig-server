package main

import (
	"log"
	"net/http"
	"path/filepath"

	"golang.org/x/crypto/acme/autocert"
)

func ListenAndServeTLS(handler http.Handler, host, email string) error {
	certsDir := filepath.Join(RootDir, "certs")

	m := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		Cache:      autocert.DirCache(certsDir),
		HostPolicy: autocert.HostWhitelist(host),
	}
	if email != "" {
		m.Email = email
	}
	s := &http.Server{
		Addr:      ":443",
		Handler:   handler,
		TLSConfig: m.TLSConfig(),
	}

	// Listen on port 80 for HTTPS challenge responses, otherwise redirect to HTTPS
	log.Printf("Listening on :80")
	go http.ListenAndServe(":80", m.HTTPHandler(nil))

	log.Printf("Listening on %s", s.Addr)
	return s.ListenAndServeTLS("", "")
}
