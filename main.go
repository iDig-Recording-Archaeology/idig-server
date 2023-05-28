package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// Global state controlled by CLI flags
var (
	ListenAddr string
	ListenAll  bool
	ListenPort int
	Verbose    bool
)

type Command struct {
	Name string
	Help string
	Func func(string, []string) error
}

var commands = []Command{
	{"start", "Start iDig Server", startCmd},
	{"create", "Create a new project", createCmd},
	{"adduser", "Add a user to a project", addUserCmd},
	{"deluser", "Delete a user from a project", delUserCmd},
	{"listusers", "List all users in a project", listUsersCmd},
	{"import", "Import a Preferences file", importCmd},
	{"log", "List versions", logCmd},
	{"rollback", "Rollback to a previous version", rollbackCmd},
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
	gin.SetMode(gin.ReleaseMode)

	var rootDir string
	if val := os.Getenv("IDIG_SERVER_DIR"); val != "" {
		rootDir = val
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		rootDir = filepath.Join(home, "iDig")
		if err := os.MkdirAll(rootDir, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	done := false

	for _, c := range commands {
		if c.Name == cmd {
			err := c.Func(rootDir, args)
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
