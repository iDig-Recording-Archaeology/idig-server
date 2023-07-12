package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

type Server struct {
	RootDir string

	r *gin.Engine
}

func NewServer(rootDir string) *Server {
	s := &Server{RootDir: rootDir}

	s.r = gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowMethods = []string{"PUT", "POST", "GET"}
	config.AllowHeaders = []string{"*"}
	s.r.Use(cors.New(config))

	s.Handle(http.MethodGet, "/idig", s.ListTrenches)
	s.HandleTrench(http.MethodPost, "/idig/:project/:trench", s.SyncTrench)
	s.HandleTrench(http.MethodGet, "/idig/:project/:trench/attachments/:name", s.ReadAttachment)
	s.HandleTrench(http.MethodPut, "/idig/:project/:trench/attachments/:name", s.WriteAttachment)
	s.HandleTrench(http.MethodGet, "/idig/:project/:trench/surveys", s.ReadSurveys)
	s.HandleTrench(http.MethodGet, "/idig/:project/:trench/surveys/:uuid/versions", s.ReadSurveyVersions)
	s.HandleTrench(http.MethodGet, "/idig/:project/:trench/versions", s.ListVersions)
	return s
}

type HandlerFunc func(*gin.Context) (int, any)

func (s *Server) Handle(httpMethod, relativePath string, handler HandlerFunc) gin.IRoutes {
	h := func(c *gin.Context) {
		code, resp := handler(c)
		if resp == nil {
			c.Status(code)
		} else if err, ok := resp.(error); ok {
			c.JSON(code, map[string]string{"error": err.Error()})
		} else {
			c.JSON(code, resp)
		}
	}
	return s.r.Handle(httpMethod, relativePath, h)
}

type TrenchHandlerFunc func(*gin.Context, *Backend) (int, any)

func (s *Server) HandleTrench(httpMethod, relativePath string, handler TrenchHandlerFunc) gin.IRoutes {
	h := func(c *gin.Context) {
		user, password, ok := c.Request.BasicAuth()
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		project := c.Param("project")
		projectDir := filepath.Join(s.RootDir, project)

		userDB, err := NewUserDB(projectDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		if !userDB.HasAccess(user, password) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		trench := c.Param("trench")
		b, err := NewBackend(projectDir, user, trench)
		if err != nil {
			c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		b.ReadOnly = !userDB.CanWriteTrench(user, trench)

		code, resp := handler(c, b)
		if resp == nil {
			c.Status(code)
		} else if err, ok := resp.(error); ok {
			c.JSON(code, map[string]string{"error": err.Error()})
		} else {
			c.JSON(code, resp)
		}
	}
	return s.r.Handle(httpMethod, relativePath, h)
}

type ListTrenchesResponse struct {
	Trenches []Trench `json:"trenches"`
}

type Trench struct {
	Project      string    `json:"project"`
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	LastModified time.Time `json:"last_modified"`
	ReadOnly     bool      `json:"read_only"`
}

func (s *Server) ListTrenches(c *gin.Context) (int, any) {
	user, password, ok := c.Request.BasicAuth()
	if !ok {
		return http.StatusUnauthorized, nil
	}

	projects, err := os.ReadDir(s.RootDir)
	if err != nil {
		return http.StatusInternalServerError, fmt.Errorf("Failed to list trenches")
	}

	trenches := []Trench{}

	for _, p := range projects {
		project := p.Name()
		projectDir := filepath.Join(s.RootDir, project)
		userDB, err := NewUserDB(projectDir)
		if err != nil {
			continue
		}

		if !userDB.HasAccess(user, password) {
			continue
		}

		entries, err := os.ReadDir(projectDir)
		if err != nil {
			return http.StatusInternalServerError, fmt.Errorf("Failed to list trenches")
		}

		for _, e := range entries {
			trench := e.Name()
			b, err := NewBackend(projectDir, user, trench)
			if err != nil {
				continue
			}
			b.ReadOnly = !userDB.CanWriteTrench(user, trench)

			v, err := b.Version()
			if err != nil {
				log.Printf("Error getting version of %s", trench)
				continue
			}
			t := Trench{
				Project:      project,
				Name:         trench,
				Version:      v.Version,
				LastModified: v.Date,
				ReadOnly:     b.ReadOnly,
			}
			trenches = append(trenches, t)
		}
	}

	return http.StatusOK, &ListTrenchesResponse{Trenches: trenches}
}

type SyncRequest struct {
	Device      string   `json:"device"`      // Device name making the request
	Message     string   `json:"message"`     // Commit message (can be empty)
	Head        string   `json:"head"`        // Client's last sync version (can be empty)
	Preferences []byte   `json:"preferences"` // Preferences file serialized
	Surveys     []Survey `json:"surveys"`     // Surveys to be committed
}

func (r SyncRequest) String() string {
	return fmt.Sprintf("{head: %s, device: %s, surveys: [%d surveys]}",
		Prefix(r.Head, 7), r.Device, len(r.Surveys))
}

type SyncResponse struct {
	Status      string   `json:"status"`                // One of: ok, pushed, missing, pull
	Version     string   `json:"version"`               // Current version of the server
	Preferences []byte   `json:"preferences,omitempty"` // Serialized preferences if different
	Missing     []string `json:"missing,omitempty"`     // List of missing attachments
	Updates     []Patch  `json:"updates,omitempty"`     // List of patches need to be applied on the client
}

func (r SyncResponse) String() string {
	version := Prefix(r.Version, 7)
	if version == "" {
		version = "-"
	}
	s := fmt.Sprintf("{status: %s, version: %s", r.Status, version)
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

// Sync Status
const (
	StatusOK        = "ok"        // Client is already in sync
	StatusPushed    = "pushed"    // New version committed
	StatusPull      = "pull"      // Client is in an older version and needs to update
	StatusMissing   = "missing"   // Some attachments are missing and need to be uploaded first
	StatusForbidden = "forbidden" // Client does not have write access
)

type Patch struct {
	Id  string `json:"id"`
	Old Survey `json:"old"`
	New Survey `json:"new"`
}

func (s *Server) SyncTrench(c *gin.Context, b *Backend) (int, any) {
	var req SyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return http.StatusBadRequest, err
	}

	log.Printf("> SYNC %s %s", b.Trench, req)

	head := b.Head()

	// When our head is empty, we let the client push. This could happen if they
	// had synced before and we somehow reset our state, or when they are starting
	// from scratch.
	if head != "" && req.Head != head {
		// Ignore errors here, we just fallback to empty values
		oldSurveys, _ := b.ReadSurveysAtVersion(req.Head)
		oldPrefs, _ := b.ReadPreferencesAtVersion(req.Head)

		newSurveys, err := b.ReadSurveys()
		if err != nil {
			return http.StatusInternalServerError, err
		}
		newPrefs, err := b.ReadPreferences()
		if err != nil {
			return http.StatusInternalServerError, err
		}

		patches := diffSurveys(oldSurveys, newSurveys)
		if bytes.Equal(oldPrefs, newPrefs) {
			newPrefs = nil // Don't send preferences if they haven't changed
		}

		resp := SyncResponse{
			Status:      StatusPull,
			Version:     head,
			Updates:     patches,
			Preferences: newPrefs,
		}

		log.Printf("< SYNC %s %s", b.Trench, resp)
		return http.StatusOK, &resp
	}

	if b.ReadOnly {
		surveys, err := b.ReadSurveys()
		if err != nil {
			return http.StatusInternalServerError, err
		}
		preferences, err := b.ReadPreferences()
		if err != nil {
			return http.StatusInternalServerError, err
		}

		patches := diffSurveys(req.Surveys, surveys)
		if bytes.Equal(req.Preferences, preferences) {
			preferences = nil // Don't send preferences if they haven't changed
		}

		var status string
		if len(patches) == 0 {
			status = StatusOK
		} else {
			status = StatusForbidden
		}

		resp := SyncResponse{
			Status:      status,
			Version:     head,
			Updates:     patches,
			Preferences: preferences,
		}

		log.Printf("< SYNC %s %s", b.Trench, resp)
		return http.StatusOK, &resp
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
		return http.StatusOK, &resp
	}

	newHead, err := b.WriteTrench(req.Device, req.Message, req.Preferences, req.Surveys)
	if err != nil {
		return http.StatusBadRequest, err
	}

	resp := SyncResponse{Version: newHead}
	if newHead != head {
		resp.Status = StatusPushed
	} else {
		resp.Status = StatusOK
	}
	log.Printf("< SYNC %s %s", b.Trench, resp)
	return http.StatusOK, &resp
}

func diffSurveys(old, new []Survey) []Patch {
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
	return patches
}

func (s *Server) ReadAttachment(c *gin.Context, b *Backend) (int, any) {
	name := c.Param("name")
	if name == "" {
		return http.StatusBadRequest, fmt.Errorf("Missing attachment name")
	}
	checksum, _ := c.GetQuery("checksum")
	if checksum == "" {
		return http.StatusBadRequest, fmt.Errorf("Missing attachment checksum")
	}

	data, err := b.ReadAttachment(name, checksum)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Data(http.StatusOK, contentType, data)
	return http.StatusOK, nil
}

func (s *Server) WriteAttachment(c *gin.Context, b *Backend) (int, any) {
	defer func() {
		// Drain any leftovers and close
		_, _ = io.Copy(io.Discard, c.Request.Body)
		c.Request.Body.Close()
	}()

	name := c.Param("name")
	if name == "" {
		return http.StatusBadRequest, fmt.Errorf("Missing attachment name")
	}
	checksum, _ := c.GetQuery("checksum")
	if checksum == "" {
		return http.StatusBadRequest, fmt.Errorf("Missing attachment checksum")
	}

	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return http.StatusBadRequest, err
	}

	err = b.WriteAttachment(name, checksum, data)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	return http.StatusOK, nil
}

type ReadSurveysResponse struct {
	Version string   `json:"version"`
	Surveys []Survey `json:"surveys"`
}

func (s *Server) ReadSurveys(c *gin.Context, b *Backend) (int, any) {
	version, _ := c.GetQuery("version")
	if version == "" {
		version = b.Head()
	}
	surveys, err := b.ReadSurveysAtVersion(version)
	if err != nil {
		return http.StatusInternalServerError, err
	}

	resp := ReadSurveysResponse{
		Version: version,
		Surveys: surveys,
	}
	return http.StatusOK, &resp
}

func (s *Server) ReadSurveyVersions(c *gin.Context, b *Backend) (int, any) {
	id := c.Param("uuid")
	versions, err := b.ReadAllSurveyVersions(id)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, versions
}

func (s *Server) ListVersions(c *gin.Context, b *Backend) (int, any) {
	versions, err := b.ListVersions()
	if err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, versions
}
