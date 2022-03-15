package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type UserDB struct {
	db map[string]string
}

func NewUserDB(passwdFile string) (*UserDB, error) {
	f, err := os.Open(passwdFile)
	if err != nil {
		return nil, fmt.Errorf("Invalid password file: %w", err)
	}
	defer f.Close()

	db := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		u, p := Cut(line, ":")
		db[u] = p
	}
	return &UserDB{db: db}, sc.Err()
}

func (u *UserDB) HasAccess(user, password string) bool {
	return u.db[user] == password
}
