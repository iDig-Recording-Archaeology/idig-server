package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"golang.org/x/crypto/acme/autocert"
)

func startCmd(args []string) error {
	stderr := log.New(os.Stderr, "", 0)
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.IntVar(&ListenPort, "p", 0, "")
	fs.BoolVar(&ListenAll, "a", false, "")
	fs.StringVar(&ListenAddr, "A", "", "")
	fs.StringVar(&HostName, "tls", "", "")
	fs.StringVar(&ContactEmail, "contact-email", "", "")
	fs.StringVar(&CertsDir, "certs-dir", "", "")
	fs.BoolVar(&Verbose, "v", false, "")
	fs.Usage = func() {
		stderr.Println("Usage: idig-server run")
		stderr.Println("  -p PORT  Port to listen on (default: 9000)")
		stderr.Println("  -A ADDR  Address to listen on (default: localhost)")
		stderr.Println("  -a       Listen on all addresses")
		stderr.Println("  -v       Enable verbose logging")
		stderr.Println()
		stderr.Println("To enable TLS use:")
		stderr.Println("  --tls HOST             Serve TLS with auto-generated certificate for this hostname")
		stderr.Println("  --contact-email EMAIL  Contact email for certificate registration")
		stderr.Println("  --certs-dir DIR        Directory to store certificate information")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if Verbose {
		log.SetFlags(log.Lshortfile)
	}

	// Check if there is at least one Project in RootDir
	entries, err := os.ReadDir(RootDir)
	if err != nil {
		return fmt.Errorf("Failed to read contents of root directory: %s", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		project := e.Name()
		usersFile := filepath.Join(RootDir, project, "users.txt")
		if !FileExists(usersFile) {
			continue
		}
		lines, err := ReadLines(usersFile)
		if err != nil {
			return fmt.Errorf("Failed to read users file for project '%s': %s", project, err)
		}
		hasUsers := false
		for _, line := range lines {
			if !strings.HasPrefix(line, "#") && strings.Contains(line, ":") {
				hasUsers = true
				break
			}
		}
		if !hasUsers {
			stderr.Printf("Warning: Project '%s' does not have any users defined.", project)
			stderr.Printf("Add a new user with: idig-server adduser %s <USER> <PASSWORD>", project)
		}
	}

	if ListenAddr == "" && ListenPort == 0 && !ListenAll && HostName == "" {
		// No networking arguments were given, use default values
		ListenAll = true
		ListenPort = 9000
	}
	if ListenAll {
		ListenAddr = "0.0.0.0"
	} else if ListenAddr == "" && HostName == "" {
		// If neither of -A, -a or -s were given, then listen on localhost only
		ListenAddr = "127.0.0.1"
	}
	if CertsDir == "" {
		CertsDir = filepath.Join(RootDir, "certs")
	}

	r := mux.NewRouter()
	r.HandleFunc("/idig", ListTrenches).Methods("GET")
	addRoute(r, "POST", "/idig/{project}/{trench}", SyncTrench)
	addRoute(r, "GET", "/idig/{project}/{trench}/attachments/{name}", ReadAttachment)
	addRoute(r, "PUT", "/idig/{project}/{trench}/attachments/{name}", WriteAttachment)
	addRoute(r, "GET", "/idig/{project}/{trench}/surveys", ReadSurveys)
	addRoute(r, "GET", "/idig/{project}/{trench}/surveys/{uuid}/versions", ReadSurveyVersions)
	addRoute(r, "GET", "/idig/{project}/{trench}/versions", ListVersions)

	// Fallback
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s [404 Not Found]", r.Method, r.URL)
		http.Error(w, "Not Found", http.StatusNotFound)
	})

	rr := handlers.CORS(
		handlers.AllowedHeaders([]string{"Authorization"}),
	)(r)

	srv := &http.Server{
		ReadHeaderTimeout: 60 * time.Second,
		IdleTimeout:       120 * time.Second,
		Handler:           rr,
	}

	if HostName != "" {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(CertsDir),
			HostPolicy: autocert.HostWhitelist(HostName),
			Email:      ContactEmail,
		}
		srv.Addr = fmt.Sprintf("%s:443", ListenAddr)
		srv.TLSConfig = m.TLSConfig()

		// Listen on port 80 for HTTPS challenge responses, otherwise redirect to HTTPS
		go http.ListenAndServe(":80", m.HTTPHandler(nil))
		log.Printf("iDig can connect to this server at: https://%s\n", HostName)
		return srv.ListenAndServeTLS("", "")
	} else {
		srv.Addr = fmt.Sprintf("%s:%d", ListenAddr, ListenPort)

		ip := ListenAddr
		if ip == "0.0.0.0" {
			if outboundIP, err := GetOutboundIP(); err == nil {
				ip = outboundIP.String()
			}
		}

		hostname, _ := os.Hostname()

		log.Print("iDig can connect to this server at:")
		if ListenPort != 80 {
			log.Printf("  http://%s:%d", ip, ListenPort)
			if hostname != "" {
				log.Printf("  http://%s:%d", hostname, ListenPort)
			}
		} else {
			log.Printf("  http://%s", ip)
			if hostname != "" {
				log.Printf("  http://%s", hostname)
			}
		}
		return srv.ListenAndServe()
	}
}

func createCmd(args []string) error {
	if len(args) != 1 {
		log.Println("Usage: idig-server create <PROJECT>")
		log.Println("e.g.: idig-server create Agora")
		os.Exit(1)
	}

	project := args[0]
	projectDir := filepath.Join(RootDir, project)
	usersFile := filepath.Join(projectDir, "users.txt")
	if FileExists(usersFile) {
		return fmt.Errorf("Project '%s' already exists", project)
	}

	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}

	err := os.WriteFile(usersFile, []byte(UsersTxtHeader), 0o644)
	if err != nil {
		return fmt.Errorf("Error creating project '%s': %s", project, err)
	}
	return nil
}

func addUserCmd(args []string) error {
	if len(args) != 3 {
		log.Println("Usage: idig-server adduser <PROJECT> <USER> <PASSWORD>")
		log.Println("e.g.: idig-server adduser Agora bruce password1")
		os.Exit(1)
	}

	project := args[0]
	user := args[1]
	password := args[2]
	hashed, _ := HashPassword(password)
	projectDir := filepath.Join(RootDir, project)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return err
	}

	usersFile := filepath.Join(projectDir, "users.txt")
	if !FileExists(usersFile) {
		err := os.WriteFile(usersFile, []byte(UsersTxtHeader), 0o644)
		if err != nil {
			return fmt.Errorf("Error creating users file: %s", err)
		}
	}

	lines, err := ReadLines(usersFile)
	if err != nil {
		return fmt.Errorf("Error reading users file: %s", err)
	}

	var out []string
	exists := false

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		u, p, _ := strings.Cut(line, ":")

		if u == user {
			exists = true
			if CheckPasswordHash(password, p) {
				log.Fatalf("User '%s' already exists with this password", user)
			} else {
				out = append(out, fmt.Sprintf("%s:%s", user, hashed))
			}
		} else {
			out = append(out, line)
		}
	}

	if !exists {
		out = append(out, fmt.Sprintf("%s:%s", user, hashed))
	}

	data := []byte(strings.Join(out, "\n") + "\n")
	if err := os.WriteFile(usersFile, data, 0o644); err != nil {
		return fmt.Errorf("Failed to write users file: %s", err)
	}

	if exists {
		log.Printf("Updated password of user '%s'", user)
	} else {
		log.Printf("Added user '%s'", user)
	}
	return nil
}

func delUserCmd(args []string) error {
	if len(args) != 2 {
		log.Println("Usage: idig-server deluser <PROJECT> <USER>")
		log.Println("e.g.: idig-server deluser Agora bruce")
		os.Exit(1)
	}

	project := args[0]
	user := args[1]
	usersFile := filepath.Join(RootDir, project, "users.txt")
	lines, err := ReadLines(usersFile)
	if err != nil {
		return fmt.Errorf("Error reading users file: %s", err)
	}

	var out []string
	exists := false
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		u, _, _ := strings.Cut(line, ":")
		if u == user {
			exists = true
		} else {
			out = append(out, line)
		}
	}

	if !exists {
		return fmt.Errorf("User '%s' does not exist", user)
	}

	data := []byte(strings.Join(out, "\n") + "\n")
	if err := os.WriteFile(usersFile, data, 0o644); err != nil {
		return fmt.Errorf("Failed to write users file: %s", err)
	}
	return nil
}

func importCmd(args []string) error {
	if len(args) != 2 {
		log.Println("Usage: idig-server import <PROJECT>/<TRENCH> <PREFERENCES FILE>")
		log.Println("e.g.: idig-server import Agora/BZ /tmp/Preferences.json")
		os.Exit(1)
	}

	project, trench, _ := strings.Cut(args[0], "/")
	preferences := args[1]
	data, err := os.ReadFile(preferences)
	if err != nil {
		return fmt.Errorf("Error reading preferences file: %s", err)
	}

	projectDir := filepath.Join(RootDir, project)
	b, err := NewBackend(projectDir, "admin", trench)
	if err != nil {
		return fmt.Errorf("Error opening trench: %s", err)
	}
	err = b.WritePreferences(data)
	if err != nil {
		return fmt.Errorf("Error writing preferences file: %s", err)
	}
	return nil
}

func listUsersCmd(args []string) error {
	if len(args) != 1 {
		log.Println("Usage: idig-server listusers <PROJECT>")
		log.Println("e.g.: idig-server listusers Agora")
		os.Exit(1)
	}

	project := args[0]
	usersFile := filepath.Join(RootDir, project, "users.txt")
	lines, err := ReadLines(usersFile)
	if err != nil {
		return fmt.Errorf("Error reading users file: %s", err)
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		u := strings.Split(line, ":")[0]
		log.Printf("%s", u)
	}
	return nil
}
