package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const UsersTxtHeader = `# Lines starting with # are ignored`

func startCmd(rootDir string, args []string) error {
	stderr := log.New(os.Stderr, "", 0)
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.IntVar(&ListenPort, "p", 0, "")
	fs.BoolVar(&ListenAll, "a", false, "")
	fs.StringVar(&ListenAddr, "A", "", "")
	fs.BoolVar(&Verbose, "v", false, "")
	fs.Usage = func() {
		stderr.Println("Usage: idig-server run")
		stderr.Println("  -p PORT  Port to listen on (default: 9000)")
		stderr.Println("  -A ADDR  Address to listen on (default: localhost)")
		stderr.Println("  -a       Listen on all addresses")
		stderr.Println("  -v       Enable verbose logging")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if Verbose {
		log.SetFlags(log.Lshortfile)
	}

	// Check if there is at least one Project in RootDir
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return fmt.Errorf("Failed to read contents of root directory: %s", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		project := e.Name()
		usersFile := filepath.Join(rootDir, project, "users.txt")
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

	if ListenAddr == "" && ListenPort == 0 && !ListenAll {
		// No networking arguments were given, use default values
		ListenAll = true
		ListenPort = 9000
	}
	if ListenAll {
		ListenAddr = "0.0.0.0"
	} else if ListenAddr == "" {
		// If neither of -A, -a were given, then listen on localhost only
		ListenAddr = "127.0.0.1"
	}

	ip := ListenAddr
	if ip == "0.0.0.0" {
		if outboundIP, err := GetOutboundIP(); err == nil {
			ip = outboundIP.String()
		}
	}

	hostname, _ := os.Hostname()

	log.Print("iDig can connect to this server at:")
	log.Printf("  http://%s:%d", ip, ListenPort)
	if hostname != "" {
		log.Printf("  http://%s:%d", hostname, ListenPort)
	}

	s := NewServer(rootDir)
	addr := fmt.Sprintf("%s:%d", ListenAddr, ListenPort)
	return s.r.Run(addr)
}

func createCmd(rootDir string, args []string) error {
	if len(args) != 1 {
		log.Println("Usage: idig-server create <PROJECT>")
		log.Println("e.g.: idig-server create Agora")
		os.Exit(1)
	}

	project := args[0]
	projectDir := filepath.Join(rootDir, project)
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

func addUserCmd(rootDir string, args []string) error {
	if len(args) != 3 {
		log.Println("Usage: idig-server adduser <PROJECT> <USER> <PASSWORD>")
		log.Println("e.g.: idig-server adduser Agora bruce password1")
		os.Exit(1)
	}

	project := args[0]
	user := args[1]
	password := args[2]
	hashed, _ := HashPassword(password)
	projectDir := filepath.Join(rootDir, project)
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
		u, _, _ := strings.Cut(line, ":")

		if u == user {
			exists = true
			log.Fatalf("User '%s' already exists", user)
		} else {
			out = append(out, line)
		}
	}

	if !exists {
		out = append(out, fmt.Sprintf("%s:%s:*", user, hashed))
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

func delUserCmd(rootDir string, args []string) error {
	if len(args) != 2 {
		log.Println("Usage: idig-server deluser <PROJECT> <USER>")
		log.Println("e.g.: idig-server deluser Agora bruce")
		os.Exit(1)
	}

	project := args[0]
	user := args[1]
	usersFile := filepath.Join(rootDir, project, "users.txt")
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

func importCmd(rootDir string, args []string) error {
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

	projectDir := filepath.Join(rootDir, project)
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

func listUsersCmd(rootDir string, args []string) error {
	if len(args) != 1 {
		log.Println("Usage: idig-server listusers <PROJECT>")
		log.Println("e.g.: idig-server listusers Agora")
		os.Exit(1)
	}

	project := args[0]
	usersFile := filepath.Join(rootDir, project, "users.txt")
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

func logCmd(rootDir string, args []string) error {
	if len(args) != 1 {
		log.Println("Usage: idig-server log <PROJECT>/<TRENCH>")
		log.Println("e.g.: idig-server log Agora/BZ")
		os.Exit(1)
	}

	project, trench, _ := strings.Cut(args[0], "/")
	projectDir := filepath.Join(rootDir, project)
	b, err := NewBackend(projectDir, "admin", trench)
	if err != nil {
		return fmt.Errorf("Error opening trench: %s", err)
	}

	versions, err := b.ListVersions()
	if err != nil {
		return err
	}

	for _, v := range versions {
		ts := v.Date.Format(time.DateTime)
		version := Prefix(v.Version, 7)
		fmt.Printf("%s  %s\n", ts, version)
	}

	return nil
}

func rollbackCmd(rootDir string, args []string) error {
	if len(args) != 2 {
		log.Println("Usage: idig-server rollback <PROJECT>/<TRENCH> <VERSION>")
		log.Println("e.g.: idig-server rollback Agora/BZ 48aba5c")
		os.Exit(1)
	}

	project, trench, _ := strings.Cut(args[0], "/")
	version := args[1]

	projectDir := filepath.Join(rootDir, project)
	b, err := NewBackend(projectDir, "admin", trench)
	if err != nil {
		return fmt.Errorf("Error opening trench: %s", err)
	}

	return b.Rollback(version)
}
