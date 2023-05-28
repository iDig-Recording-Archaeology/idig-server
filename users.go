package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type UserDB struct {
	db map[string]*User
}

type User struct {
	Name         string
	PasswordHash []byte
	Access       []string // List of trenches with read-write access
}

func NewUserDB(projectDir string) (*UserDB, error) {
	usersFile := filepath.Join(projectDir, "users.txt")
	f, err := os.Open(usersFile)
	if err != nil {
		return nil, fmt.Errorf("Invalid users file: %w", err)
	}
	defer f.Close()

	db := make(map[string]*User)
	sc := bufio.NewScanner(f)
	lineno := 0

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		lineno++

		if strings.HasPrefix(line, "#") {
			continue
		}
		t := strings.Split(line, ":")
		if len(t) < 2 {
			log.Printf("Syntax error at %s:%d", usersFile, lineno)
			continue
		}

		name := t[0]
		passwordHash := []byte(t[1])
		access := []string{}

		if len(t) >= 3 {
			for _, trench := range strings.Split(t[2], ",") {
				access = append(access, strings.TrimSpace(trench))
			}
		} else {
			// Legacy file, assume write access to all trenches
			access = append(access, "*")
		}

		db[name] = &User{Name: name, PasswordHash: passwordHash, Access: access}
	}

	return &UserDB{db: db}, sc.Err()
}

func (udb *UserDB) HasAccess(user, password string) bool {
	u := udb.db[user]
	if u == nil {
		return false
	}
	err := bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(password))
	return err == nil
}

func (udb *UserDB) CanWriteTrench(user, trench string) bool {
	u := udb.db[user]
	if u == nil {
		return false
	}

	for _, t := range u.Access {
		if t == trench || t == "*" {
			return true
		}
	}

	return false
}
