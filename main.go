package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	rootFlag := flag.String("r", ".", "Root dir of Git repositories")
	portFlag := flag.Int("p", 9000, "Listen on this port")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *portFlag)
	srv := &Server{Root: *rootFlag}
	s := &http.Server{
		Addr:    addr,
		Handler: srv,
	}

	log.Printf("Listening on %s", addr)
	log.Fatal(s.ListenAndServe())
}
