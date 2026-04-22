package web

import (
	"encoding/json"
	"net/http"

	"fiber-tracker/internal/config"
	"fiber-tracker/internal/excel"
	"fiber-tracker/internal/gdrive"
)

// ---- Google Drive API ----

func (s *Server) handleDriveStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	cfg := config.Get()
	configured := s.driveClient != nil && s.driveClient.IsConfigured()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"configured":  configured,
		"folder_id":   cfg.GDriveFolderID,
		"folder_name": cfg.GDriveFolderName,
		"enabled":     cfg.GDriveEnabled,
	})
}

func (s *Server) handleDriveAuthURL(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func (s *Server) handleDriveCallback(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/#settings", http.StatusFound)
}

func (s *Server) handleDriveDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.driveClient != nil {
		s.driveClient.SetFolder("", "")
	}
	cfg := config.Get()
	cfg.GDriveFolderID = ""
	cfg.GDriveFolderName = ""
	cfg.GDriveEnabled = false
	config.Save(cfg)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
}

func (s *Server) handleDriveFolders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]struct{}{})
}

func (s *Server) handleDriveFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.driveClient == nil || !s.driveClient.IsConfigured() {
		json.NewEncoder(w).Encode(map[string]string{"error": "Aucun dossier configuré"})
		return
	}
	files, err := s.driveClient.ListFiles()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"count": len(files), "files": files})
}

func (s *Server) handleDriveSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.driveClient == nil || !s.driveClient.IsConfigured() {
		json.NewEncoder(w).Encode(map[string]string{"error": "Aucun dossier configuré"})
		return
	}
	cfg := config.Get()
	downloaded, err := s.driveClient.SyncFolder(cfg.WatchFolder)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	for _, path := range downloaded {
		stats, err := excel.Parse(path)
		if err == nil {
			s.UpdateStats(stats)
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok", "downloaded": len(downloaded), "files": downloaded,
	})
}

func (s *Server) handleDriveSetFolder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Link == "" {
		http.Error(w, `{"error":"provide a link"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	folderID := gdrive.ParseFolderLink(req.Link)
	if folderID == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Lien invalide — collez un lien de dossier Google Drive"})
		return
	}
	if s.driveClient != nil {
		s.driveClient.SetFolder(folderID, "")
	}
	if s.driveClient != nil {
		if err := s.driveClient.TestConnection(); err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "Dossier inaccessible: " + err.Error()})
			return
		}
	}
	cfg := config.Get()
	cfg.GDriveFolderID = folderID
	cfg.GDriveFolderName = "Drive: " + folderID[:8] + "..."
	cfg.GDriveEnabled = true
	if err := config.Save(cfg); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "folder_id": folderID})
}
