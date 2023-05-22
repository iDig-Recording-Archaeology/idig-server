package main

import (
	"bufio"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "embed"

	"github.com/gorilla/mux"
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
	Verbose      bool
)

const UsersTxtHeader = `# Lines starting with # are ignored
# Format is:
#   USER:PASSWORD
`

type Command struct {
	Name string
	Help string
	Func func([]string) error
}

var commands = []Command{
	{"start", "Start iDig Server", startCmd},
	{"create", "Create a new project", createCmd},
	{"adduser", "Add a user to a project", addUserCmd},
	{"deluser", "Delete a user from a project", delUserCmd},
	{"listusers", "List all users in a project", listUsersCmd},
	{"import", "Import a Preferences file", importCmd},
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
		u, p, _ := strings.Cut(line, ":")

		if u == user {
			return CheckPasswordHash(password, p)
		}
	}
	return false
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	for _, cmd := range commands {
		fmt.Fprintf(os.Stderr, "  idig-server %-10s %s\n", cmd.Name, cmd.Help)
	}
	os.Exit(1)
}

func main() {
	log.SetFlags(0)

	if val := os.Getenv("IDIG_SERVER_DIR"); val != "" {
		RootDir = val
	} else {
		dir, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		RootDir = dir
	}

	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	done := false

	for _, c := range commands {
		if c.Name == cmd {
			err := c.Func(args)
			if err != nil {
				log.Fatal(err)
			}
			done = true
		}
	}

	if !done {
		fmt.Fprintf(os.Stderr, "Invalid command: %s\n", cmd)
		usage()
	}
}
