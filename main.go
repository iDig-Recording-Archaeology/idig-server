package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "embed"

	"github.com/gorilla/mux"
	"golang.org/x/crypto/acme/autocert"
)

// Global state controlled by CLI flags
var (
	CertsDir     string
	ContactEmail string
	HostName     string
	ListenAddr   string
	ListenAll    bool
	ListenPort   int
	RootDir      string
)

const UsersTxtHeader = `# Lines starting with # are ignored
# Format is:
#   USER:PASSWORD
`

type SyncRequest struct {
	Device      string   `json:"device"`      // Device name making the request
	Message     string   `json:"message"`     // Commit message (can be empty)
	Head        string   `json:"head"`        // Client's last sync version (can be empty)
	Preferences []byte   `json:"preferences"` // Preferences file serialized
	Surveys     []Survey `json:"surveys"`     // Surveys to be committed
}

type SyncResponse struct {
	Status      string   `json:"status"`                // One of: ok, pushed, missing, conflict
	Version     string   `json:"version"`               // Current version of the server
	Preferences []byte   `json:"preferences,omitempty"` // Serialized preferences if different
	Missing     []string `json:"missing,omitempty"`     // List of missing attachments
	Updates     []Patch  `json:"updates,omitempty"`     // List of patches need to be applied on the client
}

// Sync Status
const (
	StatusOK       = "ok"       // Client is already in sync
	StatusPushed   = "pushed"   // New version committed
	StatusConflict = "conflict" // Client is in an older version
	StatusMissing  = "missing"  // Some attachments are missing and need to be uploaded first
)

type Patch struct {
	Id  string `json:"id"`
	Old Survey `json:"old"`
	New Survey `json:"new"`
}

func SyncTrench(w http.ResponseWriter, r *http.Request, b *Backend) error {
	var req SyncRequest
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	if err != nil {
		return err
	}

	log.Printf("> SYNC %s %s", b.Trench, req)

	head := b.Head()

	// When our head is empty, we let the client push. This could happen if they
	// had synced before and we somehow reset our state, or when they are starting
	// from scratch.
	if head != "" && req.Head != head {
		// Ignore errors here, we just fallback to empty list
		old, _ := b.ReadSurveysAtVersion(req.Head)
		new, err := b.ReadSurveys()
		if err != nil {
			return err
		}

		// Generate patches
		var patches []Patch
		oldMap := NewSurveyMap(old)
		newMap := NewSurveyMap(new)
		for id := range oldMap.IDs().Union(newMap.IDs()) {
			oldSurvey := oldMap[id]
			newSurvey := newMap[id]
			if !oldSurvey.IsEqual(newSurvey) {
				patch := Patch{Id: id, Old: oldSurvey, New: newSurvey}
				patches = append(patches, patch)
			}
		}

		resp := SyncResponse{
			Status:  StatusConflict,
			Version: head,
			Updates: patches,
		}

		oldPrefs, _ := b.ReadPreferencesAtVersion(req.Head)
		newPrefs, err := b.ReadPreferences()
		if err == nil && bytes.Compare(oldPrefs, newPrefs) != 0 {
			resp.Preferences = newPrefs
		}

		log.Printf("< SYNC %s %s", b.Trench, resp)
		return writeJSON(w, r, &resp)
	}

	// The client is in the right version, but we need to check that we
	// have all required attachments first.

	missingAttachments := make(Set)
	for _, survey := range req.Surveys {
		for _, a := range survey.Attachments() {
			if !b.ExistsAttachment(a.Name, a.Checksum) {
				missingAttachments.Insert(a.Name)
			}
		}
	}
	if len(missingAttachments) > 0 {
		resp := SyncResponse{
			Status:  StatusMissing,
			Version: head,
			Missing: missingAttachments.Array(),
		}
		log.Printf("< SYNC %s %s", b.Trench, resp)
		return writeJSON(w, r, &resp)
	}

	newHead, err := b.WriteTrench(req.Device, req.Message, req.Preferences, req.Surveys)
	if err != nil {
		return err
	}

	resp := SyncResponse{Version: newHead}
	if newHead != head {
		resp.Status = StatusPushed
	} else {
		resp.Status = StatusOK
	}
	log.Printf("< SYNC %s %s", b.Trench, resp)
	return writeJSON(w, r, &resp)
}

func ReadAttachment(w http.ResponseWriter, r *http.Request, b *Backend) error {
	vars := mux.Vars(r)
	name := vars["name"]
	if name == "" {
		return fmt.Errorf("Missing attachment name")
	}
	checksum := r.URL.Query().Get("checksum")
	if checksum == "" {
		return fmt.Errorf("Missing attachment checksum")
	}

	data, err := b.ReadAttachment(name, checksum)
	if err != nil {
		return err
	}

	ctype := mime.TypeByExtension(filepath.Ext(name))
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(http.StatusOK)

	n, err := w.Write(data)
	// We can't really return any errors at this point, just report it
	if err != nil {
		log.Printf("Error sending attachment %s: %s", name, err)
	} else if n != len(data) {
		log.Printf("Incomplete write for attachment %s (%d/%d)", name, n, len(data))
	}
	return nil
}

func WriteAttachment(w http.ResponseWriter, r *http.Request, b *Backend) error {
	defer func() {
		// Drain any leftovers and close
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
	}()

	vars := mux.Vars(r)
	name := vars["name"]
	if name == "" {
		return fmt.Errorf("Missing attachment name")
	}
	checksum := r.URL.Query().Get("checksum")
	if checksum == "" {
		return fmt.Errorf("Missing attachment checksum")
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return b.WriteAttachment(name, checksum, data)
}

type ReadSurveysResponse struct {
	Version string   `json:"version"`
	Surveys []Survey `json:"surveys"`
}

func ReadSurveys(w http.ResponseWriter, r *http.Request, b *Backend) error {
	version := r.URL.Query().Get("version")
	if version == "" {
		version = b.Head()
	}
	surveys, err := b.ReadSurveysAtVersion(version)
	if err != nil {
		return err
	}

	resp := ReadSurveysResponse{
		Version: version,
		Surveys: surveys,
	}
	return writeJSON(w, r, resp)
}

func ReadSurveyVersions(w http.ResponseWriter, r *http.Request, b *Backend) error {
	vars := mux.Vars(r)
	id := vars["uuid"]
	versions, err := b.ReadAllSurveyVersions(id)
	if err != nil {
		return err
	}
	return writeJSON(w, r, versions)
}

func ListVersions(w http.ResponseWriter, r *http.Request, b *Backend) error {
	versions, err := b.ListVersions()
	if err != nil {
		return err
	}
	return writeJSON(w, r, versions)
}

func writeJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")

	enc := json.NewEncoder(w)
	if r.URL.Query().Has("debug") {
		enc.SetIndent("", "  ")
	}

	w.WriteHeader(http.StatusOK)
	// We can't really report any errors after this point
	enc.Encode(v)
	return nil
}

func (r SyncRequest) String() string {
	return fmt.Sprintf("{head: %s, device: %s, surveys: [%d surveys]}",
		Prefix(r.Head, 7), r.Device, len(r.Surveys))
}

func (r SyncResponse) String() string {
	s := fmt.Sprintf("{status: %s, version: %s", r.Status, Prefix(r.Version, 7))
	if len(r.Missing) > 0 {
		s += fmt.Sprintf(", missing: [%d attachments]", len(r.Missing))
	}
	if len(r.Preferences) > 0 {
		s += fmt.Sprintf(", preferences: <%d bytes>", len(r.Preferences))
	}
	if len(r.Updates) > 0 {
		s += fmt.Sprintf(", updates: [%d patches]", len(r.Updates))
	}
	return s + "}"
}

type ServerHandler func(http.ResponseWriter, *http.Request, *Backend) error

func addRoute(r *mux.Router, method, path string, handler ServerHandler) {
	// Wrapper function to turn handler into http.HandleFunc compatible form
	h := func(w http.ResponseWriter, r *http.Request) {
		httpError := func(msg string, code int) {
			log.Printf("%s %s [%d %s]", r.Method, r.URL, code, msg)
			http.Error(w, msg, code)
		}

		vars := mux.Vars(r)
		project := vars["project"]
		if project == "" {
			httpError("Missing project", http.StatusNotFound)
			return
		}
		trench := vars["trench"]
		if trench == "" {
			httpError("Missing trench", http.StatusNotFound)
			return
		}

		user, password, ok := r.BasicAuth()
		if !ok {
			httpError("Missing authorization header", http.StatusUnauthorized)
			return
		}
		if !hasAccess(project, user, password) {
			httpError("Invalid username or password", http.StatusUnauthorized)
			return
		}

		log.Printf("%s %s (%s)", r.Method, r.URL, user)

		projectDir := filepath.Join(RootDir, project)
		b, err := NewBackend(projectDir, user, trench)
		if err != nil {
			msg := fmt.Sprintf("Error initializing backend for %s: %s", trench, err)
			log.Println(msg)
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		err = handler(w, r, b)
		if err != nil {
			log.Printf("ERROR %s", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	r.HandleFunc(path, h).Methods(method)
}

// Check if a user has acess to a project
func hasAccess(project, user, password string) bool {
	usersFile := filepath.Join(RootDir, project, "users.txt")
	f, err := os.Open(usersFile)
	if err != nil {
		log.Printf("Can't open users file: %s", usersFile)
		return false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		u, p := Cut(line, ":")
		if u == user {
			return p == password
		}
	}
	return false
}

func startCmd(args []string) {
	stderr := log.New(os.Stderr, "", 0)
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.StringVar(&RootDir, "r", ".", "")
	fs.IntVar(&ListenPort, "p", 0, "")
	fs.BoolVar(&ListenAll, "a", false, "")
	fs.StringVar(&ListenAddr, "A", "", "")
	fs.StringVar(&HostName, "tls", "", "")
	fs.StringVar(&ContactEmail, "contact-email", "", "")
	fs.StringVar(&CertsDir, "certs-dir", "", "")
	fs.Usage = func() {
		stderr.Println("Usage: idig-server run")
		stderr.Println("  -r DIR   Root directory (default: current directory)")
		stderr.Println("  -p PORT  Port to listen on (default: 9000)")
		stderr.Println("  -A ADDR  Address to listen on (default: localhost)")
		stderr.Println("  -a       Listen on all addresses")
		stderr.Println()
		stderr.Println("To enable TLS use:")
		stderr.Println("  --tls HOST             Serve TLS with auto-generated certificate for this hostname")
		stderr.Println("  --contact-email EMAIL  Contact email for certificate registration")
		stderr.Println("  --certs-dir DIR        Directory to store certificate information")
	}
	fs.Parse(args)

	// Check if there is at least one Project in RootDir
	entries, err := os.ReadDir(RootDir)
	if err != nil {
		stderr.Fatalf("Failed to read contents of root directory: %s", err)
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
			stderr.Fatalf("Failed to read users file for project '%s': %s", project, err)
		}
		hasUsers := false
		for _, line := range lines {
			if !strings.HasPrefix(line, "#") && strings.Index(line, ":") != -1 {
				hasUsers = true
				break
			}
		}
		if !hasUsers {
			stderr.Printf("Warning: Project '%s' does not have any users defined.", project)
			stderr.Printf("Add a new user with: idig-server adduser %s <USER> <PASSWORD>", project)
		}
	}

	if ListenAddr == "" && ListenPort == 0 && ListenAll == false && HostName == "" {
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

	srv := &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
		Handler:      r,
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
		log.Fatal(srv.ListenAndServeTLS("", ""))
	} else {
		srv.Addr = fmt.Sprintf("%s:%d", ListenAddr, ListenPort)

		ip := ListenAddr
		if ip == "0.0.0.0" {
			if outboundIP, err := GetOutboundIP(); err == nil {
				ip = outboundIP.String()
			}
		}

		if ListenPort != 80 {
			log.Printf("iDig can connect to this server at: http://%s:%d", ip, ListenPort)
		} else {
			log.Printf("iDig can connect to this server at: http://%s", ip)
		}
		log.Fatal(srv.ListenAndServe())
	}
}

func createCmd(args []string) {
	log.SetFlags(0)
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	fs.StringVar(&RootDir, "r", ".", "Root directory")
	fs.Usage = func() {
		log.Println("Usage: idig-server create <PROJECT>")
		log.Println("  -r DIR   Root directory (default: current directory)")
		log.Println()
		log.Println("e.g.: idig-server create Agora")
	}
	fs.Parse(args)

	project := fs.Arg(0)
	if project == "" {
		fs.Usage()
		os.Exit(1)
	}

	projectDir := filepath.Join(RootDir, project)
	usersFile := filepath.Join(projectDir, "users.txt")
	if FileExists(usersFile) {
		log.Fatalf("Project '%s' already exists", project)
	}

	os.MkdirAll(projectDir, 0o755)

	err := os.WriteFile(usersFile, []byte(UsersTxtHeader), 0o644)
	if err != nil {
		log.Fatalf("Error creating project '%s': %s", project, err)
	}
}

func addUserCmd(args []string) {
	log.SetFlags(0)
	fs := flag.NewFlagSet("addUser", flag.ExitOnError)
	fs.StringVar(&RootDir, "r", ".", "Root directory")
	fs.Usage = func() {
		log.Println("Usage: idig-server adduser <PROJECT> <USER> <PASSWORD>")
		log.Println("  -r DIR   Root directory (default: current directory)")
		log.Println()
		log.Println("e.g.: idig-server adduser Agora bruce password1")
	}
	fs.Parse(args)

	project := fs.Arg(0)
	user := fs.Arg(1)
	password := fs.Arg(2)
	if project == "" || user == "" || password == "" {
		fs.Usage()
		os.Exit(1)
	}

	projectDir := filepath.Join(RootDir, project)
	os.MkdirAll(projectDir, 0o755)

	usersFile := filepath.Join(projectDir, "users.txt")
	if !FileExists(usersFile) {
		err := os.WriteFile(usersFile, []byte(UsersTxtHeader), 0o644)
		if err != nil {
			log.Fatalf("Error creating users file: %s", err)
		}
	}

	lines, err := ReadLines(usersFile)
	if err != nil {
		log.Fatalf("Error reading users file: %s", err)
	}

	var out []string
	exists := false
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		u, p := Cut(line, ":")
		if u == user {
			exists = true
			if p == password {
				log.Fatalf("User '%s' already exists with this password", user)
			} else {
				out = append(out, fmt.Sprintf("%s:%s", user, password))
			}
		} else {
			out = append(out, line)
		}
	}

	if !exists {
		out = append(out, fmt.Sprintf("%s:%s", user, password))
	}

	data := []byte(strings.Join(out, "\n") + "\n")
	if err := os.WriteFile(usersFile, data, 0o644); err != nil {
		log.Fatalf("Failed to write users file: %s", err)
	}

	if exists {
		log.Printf("Updated password of user '%s'", user)
	} else {
		log.Printf("Added user '%s'", user)
	}
}

func delUserCmd(args []string) {
	log.SetFlags(0)
	fs := flag.NewFlagSet("delUser", flag.ExitOnError)
	fs.StringVar(&RootDir, "r", ".", "Root directory")
	fs.Usage = func() {
		log.Println("Usage: idig-server deluser <PROJECT> <USER>")
		log.Println("  -r DIR   Root directory (default: current directory)")
		log.Println()
		log.Println("e.g.: idig-server deluser Agora bruce")
	}
	fs.Parse(args)

	project := fs.Arg(0)
	user := fs.Arg(1)
	if project == "" || user == "" {
		fs.Usage()
		os.Exit(1)
	}

	usersFile := filepath.Join(RootDir, project, "users.txt")
	lines, err := ReadLines(usersFile)
	if err != nil {
		log.Fatalf("Error reading users file: %s", err)
	}

	var out []string
	exists := false
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		u, _ := Cut(line, ":")
		if u == user {
			exists = true
		} else {
			out = append(out, line)
		}
	}

	if !exists {
		log.Fatalf("User '%s' does not exist", user)
	}

	data := []byte(strings.Join(out, "\n") + "\n")
	if err := os.WriteFile(usersFile, data, 0o644); err != nil {
		log.Fatalf("Failed to write users file: %s", err)
	}
}

func listUsersCmd(args []string) {
	log.SetFlags(0)
	fs := flag.NewFlagSet("listUsers", flag.ExitOnError)
	fs.StringVar(&RootDir, "r", ".", "Root directory")
	fs.Usage = func() {
		log.Println("Usage: idig-server listusers <PROJECT>")
		log.Println("  -r DIR   Root directory (default: current directory)")
		log.Println()
		log.Println("e.g.: idig-server listusers Agora")
	}
	fs.Parse(args)

	project := fs.Arg(0)
	if project == "" {
		fs.Usage()
		os.Exit(1)
	}

	usersFile := filepath.Join(RootDir, project, "users.txt")
	lines, err := ReadLines(usersFile)
	if err != nil {
		log.Fatalf("Error reading users file: %s", err)
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		u, p := Cut(line, ":")
		log.Printf("%-12s %s", u, p)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  idig-server start      Start iDig Server")
	fmt.Fprintln(os.Stderr, "  idig-server create     Create a new project")
	fmt.Fprintln(os.Stderr, "  idig-server adduser    Add a user to a project")
	fmt.Fprintln(os.Stderr, "  idig-server deluser    Delete a user from a project")
	fmt.Fprintln(os.Stderr, "  idig-server listusers  List all users in a project")
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "adduser":
		addUserCmd(args)
	case "create":
		createCmd(args)
	case "deluser":
		delUserCmd(args)
	case "listusers":
		listUsersCmd(args)
	case "start":
		startCmd(args)
	default:
		fmt.Fprintf(os.Stderr, "Invalid command: %s\n", cmd)
		usage()
	}
}
